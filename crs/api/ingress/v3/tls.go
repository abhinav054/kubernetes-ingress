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

package v3

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// TLS attaches a Kubernetes TLS secret to an HAProxy frontend.
type TLS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TLSSpec `json:"spec"`
}

type TLSSpec struct {
	// Frontend is the name of the HAProxy frontend that receives the certificate.
	// +kubebuilder:validation:MinLength=1
	Frontend string `json:"frontend"`

	// SecretName is the name of a kubernetes.io/tls Secret in this resource's namespace.
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// TLSList is a list of TLS resources.
type TLSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []TLS `json:"items"`
}
