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

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	v3 "github.com/haproxytech/kubernetes-ingress/crs/api/ingress/v3"
	fakeclientsetv3 "github.com/haproxytech/kubernetes-ingress/crs/generated/api/ingress/v3/clientset/versioned/fake"
)

func TestTCPUpdateStatus(t *testing.T) {
	client := fakeclientsetv3.NewSimpleClientset(&v3.TCP{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example"},
	})
	handler := NewTCPCustomResource("", true, client)
	want := []v3.TCPBackendStatus{{
		TCPName:     "mysql",
		ServiceName: "mysql",
		ServicePort: 3306,
		BackendName: "default_svc_mysql_tcp",
	}}

	require.NoError(t, handler.updateStatus("default", "example", want, ""))

	updated, err := client.IngressV3().TCPs("default").Get(context.Background(), "example", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, want, updated.Status.Backends)
	require.Len(t, client.Actions(), 3)
	require.Equal(t, "update", client.Actions()[1].GetVerb())
	require.Equal(t, "status", client.Actions()[1].GetSubresource())
}

func TestTCPUpdateStatusSkipsUnchangedStatus(t *testing.T) {
	backends := []v3.TCPBackendStatus{{
		TCPName:     "mysql",
		ServiceName: "mysql",
		ServicePort: 3306,
		BackendName: "default_svc_mysql_tcp",
	}}
	client := fakeclientsetv3.NewSimpleClientset(&v3.TCP{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example"},
		Status:     v3.TCPStatus{Backends: backends},
	})
	handler := NewTCPCustomResource("", true, client)

	require.NoError(t, handler.updateStatus("default", "example", backends, ""))
	require.Len(t, client.Actions(), 1)
	require.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestTCPUpdateStatusRecordsErrorAndEvent(t *testing.T) {
	crClient := fakeclientsetv3.NewSimpleClientset(&v3.TCP{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example", UID: "uid-1"},
	})
	client := fake.NewSimpleClientset()
	handler := NewTCPCustomResource("", true, crClient, client)

	reconcileError := "tcp-cr: service 'default/missing' not found"
	require.NoError(t, handler.updateStatus("default", "example", nil, reconcileError))

	updated, err := crClient.IngressV3().TCPs("default").Get(context.Background(), "example", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, reconcileError, updated.Status.Error)

	events, err := client.CoreV1().Events("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 1)
	require.Equal(t, corev1.EventTypeWarning, events.Items[0].Type)
	require.Equal(t, "ReconcileError", events.Items[0].Reason)
	require.Equal(t, reconcileError, events.Items[0].Message)
}
