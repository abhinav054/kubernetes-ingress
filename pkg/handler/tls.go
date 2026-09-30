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
		originalLen := len(frontend.SSLFrontUses)
		uses := frontend.SSLFrontUses[:0]
		for _, use := range frontend.SSLFrontUses {
			if use.Metadata == nil || use.Metadata[tlsCROwnerMetadata] == nil {
				uses = append(uses, use)
			}
		}
		frontend.SSLFrontUses = uses
		if len(uses) != originalLen {
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
			frontend.SSLFrontUses = append(frontend.SSLFrontUses, &models.SSLFrontUse{
				Certificate: certPath,
				Metadata: map[string]interface{}{
					tlsCROwnerMetadata: namespace + "/" + name,
				},
			})
			dirty[frontend.Name] = struct{}{}
		}
	}
	for frontendName := range dirty {
		frontend := frontendByName[frontendName]
		errs.Add(h.FrontendEditStructured(frontendName, frontend))
	}
	return errs.Result()
}
