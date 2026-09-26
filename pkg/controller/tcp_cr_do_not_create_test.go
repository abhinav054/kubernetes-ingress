// Copyright 2026 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"testing"

	"github.com/haproxytech/client-native/v6/models"
	"github.com/stretchr/testify/require"

	v3 "github.com/haproxytech/kubernetes-ingress/crs/api/ingress/v3"
	"github.com/haproxytech/kubernetes-ingress/pkg/handler"
	"github.com/haproxytech/kubernetes-ingress/pkg/haproxy/instance"
	"github.com/haproxytech/kubernetes-ingress/pkg/store"
)

func TestTCPCRDoNotCreateOnlyReconcilesBackend(t *testing.T) {
	c := buildGlobalTestController(t)
	ns := c.store.GetNamespace("ns")
	addBackendService(t, c.store, ns, "tcp-service")

	const frontendName = "externally-managed"
	require.NoError(t, c.haproxy.APIStartTransaction())
	require.NoError(t, c.haproxy.FrontendCreate(models.FrontendBase{
		Name:           frontendName,
		Mode:           "tcp",
		Description:    "must remain unchanged",
		DefaultBackend: "existing-backend",
	}))
	require.NoError(t, c.haproxy.APICommitTransaction())
	c.haproxy.APIDisposeTransaction()
	instance.Reset()

	ns.CRs.TCPsPerCR["example"] = &store.TCPs{
		Namespace: "ns",
		Name:      "example",
		Status:    store.ADDED,
		Items: store.TCPResourceList{
			{
				Namespace:  "ns",
				ParentName: "example",
				TCPModel: v3.TCPModel{
					Name:        "entry",
					DoNotCreate: true,
					Frontend: models.Frontend{
						FrontendBase: models.FrontendBase{Name: frontendName},
					},
					Service: v3.TCPService{Name: "tcp-service", Port: 80},
				},
			},
		},
	}

	require.NoError(t, c.haproxy.APIStartTransaction())
	tcpHandler := handler.NewTCPCustomResource("", true, nil)
	require.NoError(t, tcpHandler.Update(c.store, c.haproxy, c.annotations))

	frontend, err := c.haproxy.FrontendGet(frontendName)
	require.NoError(t, err)
	require.Equal(t, "must remain unchanged", frontend.Description)
	require.Equal(t, "existing-backend", frontend.DefaultBackend)

	_, err = c.haproxy.FrontendGet("tcpcr_ns_" + frontendName)
	require.Error(t, err)

	backend, err := c.haproxy.BackendGet("ns_svc_tcp-service_http")
	require.NoError(t, err)
	require.Equal(t, "tcp", backend.Mode)

	require.NoError(t, c.haproxy.APICommitTransaction())
	c.haproxy.APIDisposeTransaction()
}
