// Copyright 2019 HAProxy Technologies LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package handler

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/haproxytech/client-native/v6/models"
	v3 "github.com/haproxytech/kubernetes-ingress/crs/api/ingress/v3"
	crclientsetv3 "github.com/haproxytech/kubernetes-ingress/crs/generated/api/ingress/v3/clientset/versioned"
	"github.com/haproxytech/kubernetes-ingress/pkg/annotations"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy/certs"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy/instance"
	rc "github.com/haproxytech/kubernetes-ingress/pkg/reference-counter"
	"github.com/haproxytech/kubernetes-ingress/pkg/rules"
	aclrules "github.com/haproxytech/kubernetes-ingress/pkg/rules/acls"
	backendswitchingrules "github.com/haproxytech/kubernetes-ingress/pkg/rules/backend_switching_rules"
	bindsrules "github.com/haproxytech/kubernetes-ingress/pkg/rules/binds"
	"github.com/haproxytech/kubernetes-ingress/pkg/rules/captures"
	"github.com/haproxytech/kubernetes-ingress/pkg/rules/filters"
	logtargets "github.com/haproxytech/kubernetes-ingress/pkg/rules/log_targets"
	tcprequestrules "github.com/haproxytech/kubernetes-ingress/pkg/rules/tcp_request_rules"
	"github.com/haproxytech/kubernetes-ingress/pkg/secret"
	"github.com/haproxytech/kubernetes-ingress/pkg/service"
	"github.com/haproxytech/kubernetes-ingress/pkg/store"
	"github.com/haproxytech/kubernetes-ingress/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const tcpServicePrefix = "tcpcr"

type TCPCustomResource struct {
	controllerIngressClass string
	allowEmptyIngressClass bool
	crClient               crclientsetv3.Interface
	client                 kubernetes.Interface
}

type tcpcontext struct {
	k         store.K8s
	namespace string
	h         haproxy.HAProxy
}

// var syncIngressClassLog sync.Once
func NewTCPCustomResource(controllerIngressClass string, allowEmptyIngressClass bool, crClient crclientsetv3.Interface, clients ...kubernetes.Interface) TCPCustomResource {
	var client kubernetes.Interface
	if len(clients) > 0 {
		client = clients[0]
	}
	return TCPCustomResource{
		controllerIngressClass: controllerIngressClass,
		allowEmptyIngressClass: allowEmptyIngressClass,
		crClient:               crClient,
		client:                 client,
	}
}

// func logTCPMigration30To31Warning() {
// 	logger := utils.GetLogger()
// 	// For 3.0, (WARNING)
// 	// Starting from 3.1, if ingress.class is set for controller, you will need to set the ingress.class annotation in the TCP CRD
// 	// - Setting the ingress.class annotation in the TCP CRD in 3.0 is highly recommended before migration to 3.1
// 	// - empty-ingress-class controller option will also impact TCP CRD starting 3.1
// 	logger.Warning("Using TCP CRD without ingress.class annotation will work only in 3.0")
// 	logger.Warning("If you are using TCP CRDS without ingress.class annotation and ingress.class is set for the controller,an action is required before migrating to 3.1")
// 	logger.Warning("Please read https://github.com/haproxytech/kubernetes-ingress/blob/master/documentation/custom-resource-tcp.md for more information")
// }

func (handler TCPCustomResource) Update(k store.K8s, h haproxy.HAProxy, a annotations.Annotations) (err error) {
	var errs utils.Errors

	for _, ns := range k.Namespaces {
		for _, tcpCR := range ns.CRs.TCPsPerCR {
			// ----------------------------------
			// ingress.class migration
			// To log in 3.0
			// Not in 3.1
			// syncIngressClassLog.Do(func() {
			// 	logTCPMigration30To31Warning()
			// })

			// >= v3.1 for ingress.class
			// Not in 3.0
			supported := handler.isSupportedIngressClass(k, tcpCR)
			if !supported {
				for _, atcp := range tcpCR.Items {
					owner := atcp.Owner()
					k.FrontendRC.RemoveOwner(owner)
				}
				continue
			}
			// end ingress.class migration
			// ----------------------------

			// Cleanup will done after Haproxy config transaction succeeds
			if tcpCR.Status == store.DELETED {
				continue
			}
			ctx := tcpcontext{
				k:         k,
				h:         h,
				namespace: ns.Name,
			}
			backendStatuses := make([]v3.TCPBackendStatus, 0)
			var reconcileErrors utils.Errors
			for _, tcp := range tcpCR.Items {
				if tcp.CollisionStatus == store.ERROR {
					logger.Errorf("tcp-cr: skipping tcp '%s/%s/%s' due to collision %s", ctx.namespace, tcp.ParentName, tcp.Name, tcp.Reason)
					continue
				}
				owner := tcp.Owner()
				errSvc := handler.checkService(ctx, tcp.TCPModel)
				if errSvc != nil {
					errs.Add(errSvc)
					reconcileErrors.Add(errSvc)
					continue
				}

				// Frontend
				backendName, errH := handler.reconcileFrontend(ctx, owner, tcp.TCPModel, a)
				if errH != nil {
					errs.Add(errH)
					reconcileErrors.Add(errH)
					continue
				}
				backendStatuses = append(backendStatuses, newTCPBackendStatus(tcp.Name, tcp.Service, backendName))

				// Additional Backends
				additionalStatuses, errBack := handler.reconcileAdditionalBackends(ctx, tcp.Name, tcp.TCPModel.Services, a)
				if errBack != nil {
					errs.Add(errBack)
					reconcileErrors.Add(errBack)
					continue
				}
				backendStatuses = append(backendStatuses, additionalStatuses...)
			}
			errorMessage := ""
			if errReconcile := reconcileErrors.Result(); errReconcile != nil {
				errorMessage = errReconcile.Error()
			}
			if errStatus := handler.updateStatus(ctx.namespace, tcpCR.Name, backendStatuses, errorMessage); errStatus != nil {
				errs.Add(errStatus)
			}
		}
	}
	handler.clearFrontends(k, h)

	return errs.Result()
}

func (handler TCPCustomResource) checkService(ctx tcpcontext, tcp v3.TCPModel) (err error) {
	var ok bool
	if _, ok = ctx.k.Namespaces[ctx.namespace]; !ok {
		err = fmt.Errorf("tcp-cr: namespace of service '%s/%s' not found", ctx.namespace, tcp.Service.Name)
		return err
	}
	_, ok = ctx.k.Namespaces[ctx.namespace].Services[tcp.Service.Name]
	if !ok {
		err = fmt.Errorf("tcp-cr: service '%s/%s' not found", ctx.namespace, tcp.Service.Name)
		return err
	}
	return nil
}

func (handler TCPCustomResource) reconcileFrontend(ctx tcpcontext, owner rc.Owner, tcp v3.TCPModel, a annotations.Annotations) (string, error) {
	if tcp.DoNotCreate {
		return handler.reconcileDefaultBackend(ctx, tcp, a, false)
	}

	cfgFrontendName := cfgFrontendName(ctx.namespace, tcp.Frontend)
	frontend := tcp.Frontend
	applyFrontendOverride(ctx.namespace, &frontend)

	if errAdd := handler.createOrEditFrontend(ctx.h, frontend); errAdd != nil {
		return "", errAdd
	}
	ctx.k.FrontendRC.AddOwner(rc.HaproxyCfgResourceName(cfgFrontendName), owner)

	if errBinds := handler.reconcileBinds(ctx, frontend, tcp.Frontend.Binds, owner); errBinds != nil {
		return "", errBinds
	}
	aclrules.PopulateFrontend(ctx.h, frontend.Name, tcp.Frontend.ACLList)
	if errBsr := backendswitchingrules.Reconcile(ctx.h, frontend.Name, tcp.Frontend.BackendSwitchingRuleList); errBsr != nil {
		return "", errBsr
	}
	if errCap := captures.Reconcile(ctx.h, frontend.Name, tcp.Frontend.CaptureList); errCap != nil {
		return "", errCap
	}
	if errFilter := filters.Reconcile(ctx.h, rules.ParentTypeFrontend, frontend.Name, tcp.Frontend.FilterList); errFilter != nil {
		return "", errFilter
	}
	if errLogTargets := logtargets.Reconcile(ctx.h, rules.ParentTypeFrontend, frontend.Name, tcp.Frontend.LogTargetList); errLogTargets != nil {
		return "", errLogTargets
	}
	if errTCPRequests := tcprequestrules.Reconcile(ctx.h, rules.ParentTypeFrontend, frontend.Name, tcp.Frontend.TCPRequestRuleList); errTCPRequests != nil {
		return "", errTCPRequests
	}

	return handler.reconcileDefaultBackend(ctx, tcp, a, true)
}

func (handler TCPCustomResource) reconcileDefaultBackend(ctx tcpcontext, tcp v3.TCPModel, a annotations.Annotations, attachToFrontend bool) (string, error) {
	path := &store.IngressPath{
		SvcNamespace:     ctx.namespace,
		SvcName:          tcp.Service.Name,
		SvcPortInt:       int64(tcp.Service.Port),
		IsDefaultBackend: attachToFrontend,
	}
	svc, err := service.New(ctx.k, path, nil, true, nil, ctx.k.ConfigMaps.Main.Annotations)
	if err != nil {
		return "", fmt.Errorf("error resolving tcp backend: %w", err)
	}
	if attachToFrontend {
		frontendName := cfgFrontendName(ctx.namespace, tcp.Frontend)
		if errSvc := svc.SetDefaultBackend(ctx.k, ctx.h, []string{frontendName}, a); errSvc != nil {
			return "", fmt.Errorf("error configuring tcp frontend: %w", errSvc)
		}
	} else {
		if errSvc := svc.HandleBackend(ctx.k, ctx.h, a); errSvc != nil {
			return "", fmt.Errorf("error configuring tcp backend: %w", errSvc)
		}
		svc.HandleHAProxySrvs(ctx.k, ctx.h)
	}
	backendName, err := svc.GetBackendName()
	if err != nil {
		return "", fmt.Errorf("error getting generated tcp backend name: %w", err)
	}
	return backendName, nil
}

func cfgFrontendName(namespace string, frontend models.Frontend) string {
	return tcpServicePrefix + "_" + namespace + "_" + frontend.Name
}

func applyFrontendOverride(namespace string, fe *models.Frontend) {
	fe.Mode = "tcp"
	// Add a "tcp-" prefix
	fe.Name = cfgFrontendName(namespace, *fe)
	// LogFormat
	format := strings.TrimSpace(fe.LogFormat)
	if format != "" {
		fe.LogFormat = "'" + strings.TrimSpace(fe.LogFormat) + "'"
	}
}

func (handler TCPCustomResource) applyBindOverride(ctx tcpcontext, bind *models.Bind, owner rc.Owner) {
	if bind.Ssl {
		if bind.SslCertificate == "" {
			bind.SslCertificate = ctx.h.Certs.FrontendDir
			return
		}
		// Does a secret with bind.SSlCertificate exists ?
		secretManager := secret.NewManager(ctx.k, ctx.h)
		certName := bind.SslCertificate
		// It might contain: a secret name (same namespace as the TCP CR) or a folder or a file.
		// If the a secret with the bind.SSlCertificate name is found in the same namespace as the TCP CR:
		// Place it into the folder and use it
		// if _, ok := ctx.k.Namespaces[ctx.namespace].Secret[bind.SslCertificate]; ok {
		if _, err := ctx.k.GetSecret(ctx.namespace, bind.SslCertificate); err == nil {
			sec := secret.Secret{
				Name:       types.NamespacedName{Namespace: ctx.namespace, Name: certName},
				SecretType: certs.TCP_CERT,
				OwnerType:  secret.OWNERTYPE_TCP_CR,
				OwnerName:  string(owner.Key()),
			}
			secretManager.Store(sec)
			bind.SslCertificate = ctx.h.Certs.TCPCRDir + fmt.Sprintf("/%s_%s", ctx.namespace, certName) + ".pem"
		}
		// If not a secret name
		// We take what ever is in bind.SslCertificate and use it (it it's a file, it has to be the complete path to the file)
		// Nothing special to do, bind.SSlCertificate is already containing the value needed (folder or file)
	}
}

func (handler TCPCustomResource) createOrEditFrontend(h haproxy.HAProxy, frontend models.Frontend) error {
	oldfe, err := h.FrontendGet(frontend.Name)
	// Create
	if err != nil {
		err = h.FrontendCreate(frontend.FrontendBase)
		if err == nil {
			instance.Reload("TCP frontend '%s' created", frontend.Name)
		}
		return err
	}

	// Update
	diffsBase := frontend.FrontendBase.Diff(oldfe.FrontendBase)
	// exclude "DefaultBackend" from the diffs, DefaultBackend is set afterwards in the flow in frontend
	// A diff in DefaultBackend is normal at this stage
	// Diffs on FrontendBase
	delete(diffsBase, "DefaultBackend")
	if len(diffsBase) != 0 {
		err = h.FrontendEdit(frontend.FrontendBase)
		if err == nil {
			instance.Reload("TCP frontend base '%s' updated %v", frontend.Name, diffsBase)
		}
		return err
	}

	return nil
}

func (handler TCPCustomResource) reconcileBinds(ctx tcpcontext, frontend models.Frontend, binds map[string]models.Bind, owner rc.Owner) error {
	newBinds := models.Binds{}
	for _, bind := range binds {
		// Copy= do not change bind in store with overrides
		abind := models.Bind{}
		b, _ := bind.MarshalBinary()
		_ = abind.UnmarshalBinary(b)
		handler.applyBindOverride(ctx, &abind, owner)
		newBinds = append(newBinds, &abind)
	}

	return bindsrules.ReconcileBinds(ctx.h, frontend.Name, newBinds)
}

func (handler TCPCustomResource) clearFrontends(k store.K8s, h haproxy.HAProxy) {
	frontends, err := h.FrontendsGet()
	if err != nil {
		logger.Error(err)
		return
	}
	for _, cfgFe := range frontends {
		isTCPServiceFe := isTCPFrontend(cfgFe)
		if !isTCPServiceFe {
			continue
		}
		isRequired := isTCPFrontendRequired(k, cfgFe.Name)

		if !isRequired {
			err = h.FrontendDelete(cfgFe.Name)
			if err != nil {
				logger.Errorf("tcp-ct: error deleting tcp frontend '%s': %s", cfgFe.Name, err)
			}
			instance.ReloadIf(err == nil, "TCP frontend '%s' deleted", cfgFe.Name)
		}
	}
}

func isTCPFrontend(frontend *models.Frontend) bool {
	return frontend.Mode == "tcp" && strings.HasPrefix(frontend.Name, tcpServicePrefix)
}

func isTCPFrontendRequired(k store.K8s, configFrontendName string) bool {
	return k.FrontendRC.HasOwners(rc.HaproxyCfgResourceName(configFrontendName))
}

func (handler TCPCustomResource) reconcileAdditionalBackends(ctx tcpcontext, tcpName string, services v3.TCPServices, a annotations.Annotations) ([]v3.TCPBackendStatus, error) {
	var errors utils.Errors
	statuses := make([]v3.TCPBackendStatus, 0, len(services))
	for _, additionalService := range services {
		path := &store.IngressPath{
			SvcNamespace:     ctx.namespace,
			SvcName:          additionalService.Name,
			SvcPortInt:       int64(additionalService.Port),
			IsDefaultBackend: false,
		}
		svc, err := service.New(ctx.k, path, nil, true, nil, ctx.k.ConfigMaps.Main.Annotations)
		if err != nil {
			errors.Add(err)
			continue
		}
		if errSvc := svc.HandleBackend(ctx.k, ctx.h, a); errSvc != nil {
			errors.Add(errSvc)
			continue
		}
		svc.HandleHAProxySrvs(ctx.k, ctx.h)
		backendName, err := svc.GetBackendName()
		if err != nil {
			errors.Add(err)
			continue
		}
		statuses = append(statuses, newTCPBackendStatus(tcpName, *additionalService, backendName))
	}
	return statuses, errors.Result()
}

func newTCPBackendStatus(tcpName string, tcpService v3.TCPService, backendName string) v3.TCPBackendStatus {
	return v3.TCPBackendStatus{
		TCPName:     tcpName,
		ServiceName: tcpService.Name,
		ServicePort: tcpService.Port,
		BackendName: backendName,
	}
}

func (handler TCPCustomResource) updateStatus(namespace, name string, backends []v3.TCPBackendStatus, errorMessage string) error {
	if handler.crClient == nil {
		return nil
	}

	tcpClient := handler.crClient.IngressV3().TCPs(namespace)
	tcp, err := tcpClient.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get TCP CR %s/%s for status update: %w", namespace, name, err)
	}
	if slices.Equal(tcp.Status.Backends, backends) && tcp.Status.Error == errorMessage {
		return nil
	}

	previousError := tcp.Status.Error
	tcp.Status.Backends = backends
	tcp.Status.Error = errorMessage
	if _, err = tcpClient.UpdateStatus(context.Background(), tcp, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update TCP CR %s/%s status: %w", namespace, name, err)
	}
	if errorMessage != "" && errorMessage != previousError {
		handler.recordWarningEvent(tcp, errorMessage)
	}
	return nil
}

func (handler TCPCustomResource) recordWarningEvent(tcp *v3.TCP, message string) {
	if handler.client == nil {
		return
	}
	now := metav1.NewTime(time.Now())
	_, err := handler.client.CoreV1().Events(tcp.Namespace).Create(context.Background(), &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: tcp.Name + "-",
			Namespace:    tcp.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "TCP",
			Namespace:  tcp.Namespace,
			Name:       tcp.Name,
			UID:        tcp.UID,
			APIVersion: v3.SchemeGroupVersion.String(),
		},
		Type:           corev1.EventTypeWarning,
		Reason:         "ReconcileError",
		Message:        message,
		FirstTimestamp: now,
		LastTimestamp:  now,
		Count:          1,
		Source: corev1.EventSource{
			Component: "haproxy-ingress",
		},
	}, metav1.CreateOptions{})
	logger.Error(err)
}

func (handler TCPCustomResource) isSupportedIngressClass(k store.K8s, tcps *store.TCPs) bool {
	supported := k.IsIngressClassSupported(tcps.IngressClass, handler.controllerIngressClass, handler.allowEmptyIngressClass)
	return supported
}
