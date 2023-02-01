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

// MongoDBClusterSpec defines the desired state of MongoDBCluster
type MongoDBClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ClusterName is the cluster name that is going to be used when requesting credentials
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// ConnectionString that Airlock will use to connect to the cluster. It should have enough privileges to manage users and access. This is not gonna be used by the created users.
	// +kubebuilder:validation:Required
	ConnectionString string `json:"connectionString"`

	// The host that clients will receive when requesting credentials.
	HostTemplate string `json:"hostTemplate,omitempty"`

	// The port that clients will receive when requesting credentials.
	// +kubebuilder:default=27017
	PortTemplate string `json:"portTemplate,omitempty"`

	// Extra connection string parameters that will be added to the connection string.
	// +kubebuilder:default=?replicaSet=rs01
	OptionsTemplate string `json:"optionsTemplate,omitempty"`
}

// MongoDBClusterStatus defines the observed state of MongoDBCluster
type MongoDBClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MongoDBCluster is the Schema for the mongodbclusters API
type MongoDBCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBClusterSpec   `json:"spec,omitempty"`
	Status MongoDBClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MongoDBClusterList contains a list of MongoDBCluster
type MongoDBClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBCluster{}, &MongoDBClusterList{})
}
