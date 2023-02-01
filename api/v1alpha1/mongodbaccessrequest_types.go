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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MongoDBAccessRequestSpec defines the desired state of MongoDBAccessRequest
type MongoDBAccessRequestSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Username to be created in the cluster.
	UserName string `json:"userName,omitempty"`

	// In which cluster to create the user.
	ClusterName string `json:"clusterName,omitempty"`

	// Password to be used for the user. If not provided, a random password will be generated.
	Password string `json:"password,omitempty"`

	// Database to be used for the user. If not provided, the user will have access to one that matches the access request name
	Database string `json:"database,omitempty"`

	// Secret name where the credentials will be stored. If not provided, will be the same as the access request name.
	SecretName string `json:"secretName,omitempty"`
}

// MongoDBAccessRequestStatus defines the observed state of MongoDBAccessRequest
type MongoDBAccessRequestStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions is the list of status condition updates
	Conditions []metav1.Condition `json:"conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MongoDBAccessRequest is the Schema for the mongodbaccessrequests API
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
