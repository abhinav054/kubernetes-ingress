// Copyright 2026 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package store

import v3 "github.com/haproxytech/kubernetes-ingress/crs/api/ingress/v3"

func (k *K8s) EventTLSCR(namespace, name string, data *v3.TLS) bool {
	ns := k.GetNamespace(namespace)
	if data == nil {
		delete(ns.CRs.TLS, name)
		return true
	}
	ns.CRs.TLS[name] = &data.Spec
	return true
}
