// Copyright 2026 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package handler

import (
	"fmt"
	"sort"

	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/kubernetes-ingress/pkg/annotations"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy/certs"
	"github.com/haproxytech/kubernetes-ingress/pkg/secret"
	"github.com/haproxytech/kubernetes-ingress/pkg/store"
	"github.com/haproxytech/kubernetes-ingress/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
)

const tlsCROwnerMetadata = "haproxy.org/tls-cr"

type TLS struct{}

func (handler TLS) Update(k store.K8s, h haproxy.HAProxy, _ annotations.Annotations) error {
	frontends, err := h.FrontendsGet()
	if err != nil {
		return err
	}
	frontendByName := make(map[string]*models.Frontend, len(frontends))
	dirty := make(map[string]struct{})
	for _, frontend := range frontends {
		frontendByName[frontend.Name] = frontend
		if removeTLSCRSSLFrontUses(frontend) {
			logger.Infof("TLS reconciliation: removed legacy ssl-f-use entries from frontend '%s'", frontend.Name)
			dirty[frontend.Name] = struct{}{}
		}
	}

	namespaces := make([]string, 0, len(k.Namespaces))
	for namespace := range k.Namespaces {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)

	secretManager := secret.NewManager(k, h)
	errs := utils.Errors{}
	for _, namespace := range namespaces {
		ns := k.Namespaces[namespace]
		names := make([]string, 0, len(ns.CRs.TLS))
		for name := range ns.CRs.TLS {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tls := ns.CRs.TLS[name]
			frontend := frontendByName[tls.Frontend]
			if frontend == nil {
				errs.Add(fmt.Errorf("TLS %s/%s: frontend %q not found", namespace, name, tls.Frontend))
				continue
			}
			logger.Infof("TLS CR '%s/%s': reconciling secret '%s/%s' on frontend '%s'",
				namespace, name, namespace, tls.SecretName, tls.Frontend)
			certPath, certErr := secretManager.StorePath(secret.Secret{
				Name:       types.NamespacedName{Namespace: namespace, Name: tls.SecretName},
				OwnerType:  secret.OWNERTYPE_TLS_CR,
				OwnerName:  name,
				SecretType: certs.FT_CERT,
			})
			if certErr != nil {
				errs.Add(fmt.Errorf("TLS %s/%s: %w", namespace, name, certErr))
				continue
			}
			logger.Infof("TLS CR '%s/%s': certificate write queued for '%s'", namespace, name, certPath)
			if len(frontend.Binds) == 0 {
				errs.Add(fmt.Errorf("TLS %s/%s: frontend %q has no binds", namespace, name, tls.Frontend))
				continue
			}
			if enableFrontendTLS(frontend, h.Certs.FrontendDir) {
				dirty[frontend.Name] = struct{}{}
			}
			for bindName, bind := range frontend.Binds {
				logger.Infof("TLS CR '%s/%s': frontend '%s' bind '%s' uses crt '%s' (ssl=%t, alpn=%q)",
					namespace, name, frontend.Name, bindName, bind.SslCertificate, bind.Ssl, bind.Alpn)
			}
		}
	}
	for frontendName := range dirty {
		frontend := frontendByName[frontendName]
		if errEdit := h.FrontendEditStructured(frontendName, frontend); errEdit != nil {
			logger.Errorf("TLS reconciliation: failed to persist frontend '%s': %v", frontendName, errEdit)
			errs.Add(errEdit)
			continue
		}
		logger.Infof("TLS reconciliation: persisted frontend '%s'", frontendName)
	}
	return errs.Result()
}

func removeTLSCRSSLFrontUses(frontend *models.Frontend) bool {
	originalLen := len(frontend.SSLFrontUses)
	uses := frontend.SSLFrontUses[:0]
	for _, use := range frontend.SSLFrontUses {
		if use.Metadata == nil || use.Metadata[tlsCROwnerMetadata] == nil {
			uses = append(uses, use)
		}
	}
	frontend.SSLFrontUses = uses
	return len(uses) != originalLen
}

func enableFrontendTLS(frontend *models.Frontend, certDir string) bool {
	changed := false
	for name, bind := range frontend.Binds {
		if !bind.Ssl {
			bind.Ssl = true
			changed = true
		}
		if bind.SslCertificate == "" {
			bind.SslCertificate = certDir
			changed = true
		}
		frontend.Binds[name] = bind
	}
	return changed
}
