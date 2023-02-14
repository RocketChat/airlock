/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MongoDBAccessRequestSpec defines the desired state of MongoDBAccessRequest
type MongoDBAccessRequestSpec struct {

	// Username to be created in the cluster. If not provided, will be the same as the access request name.
	UserName string `json:"userName,omitempty"`

	// In which cluster to create the user.
	// +kubebuilder:validation:required
	ClusterName string `json:"clusterName,omitempty"`

	// Database to be used for the user. If not provided, the user will have access to one that matches the access request name
	Database string `json:"database,omitempty"`

	// Secret name where the credentials will be stored. If not provided, will be the same as the access request name.
	SecretName string `json:"secretName,omitempty"`
}

// MongoDBAccessRequestStatus defines the observed state of MongoDBAccessRequest
type MongoDBAccessRequestStatus struct {

	// Conditions is the list of status condition updates
	Conditions []metav1.Condition `json:"conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MongoDBAccessRequest is the Schema for the mongodbaccessrequests API
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type MongoDBAccessRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBAccessRequestSpec   `json:"spec,omitempty"`
	Status MongoDBAccessRequestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MongoDBAccessRequestList contains a list of MongoDBAccessRequest
type MongoDBAccessRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBAccessRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBAccessRequest{}, &MongoDBAccessRequestList{})
}
