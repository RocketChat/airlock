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

// MongoDBClusterSpec defines the desired state of MongoDBCluster

type MongoDBClusterSpec struct {

	// Secret in which Airlock will look for a ConnectionString or Atlas credentials, that will be used to connect to the cluster. It should have enough privileges to manage users and access. This is not gonna be used by the created users.
	ConnectionSecret string `json:"connectionSecret"`

	// +kubebuilder:default=airlock-system
	// +kubebuilder:validation:Optional
	ConnectionSecretNamespace string `json:"connectionSecretNamespace,omitempty"`

	// The host with port that clients will receive when requesting credentials.
	// +kubebuilder:validation:Required
	HostTemplate string `json:"hostTemplate"` // Obs: no omitempty here to make it required. (the annotation above refuses to work on this particular field for some reason)

	// Extra connection string parameters that will be added to the connection string.
	// +kubebuilder:default=?replicaSet=rs01
	OptionsTemplate string `json:"optionsTemplate,omitempty"`

	// The prefix used when building the connection string. Defaults to "mongodb"
	// +kubebuilder:default=mongodb
	PrefixTemplate string `json:"prefixTemplate,omitempty"`

	// Append this prefix to all default/generated usernames for this cluster. Will be overriden if "username" is specified.
	UserNamePrefix string `json:"userNamePrefix,omitempty"`

	// If this is set, Atlas API will be used instead of the regular mongo auth path.
	UseAtlasApi bool `json:"useAtlasApi,omitempty"`

	// If this is set, along with useAtlasApi, all the kubernetes nodes on the cluster will be added to the Atlas firewall, using the rke.cattle.io/external-ip annotation.
	AllowOnAtlasFirewall bool `json:"allowOnAtlasFirewall,omitempty"`
}

// MongoDBClusterStatus defines the observed state of MongoDBCluster
type MongoDBClusterStatus struct {
	Conditions []metav1.Condition `json:"conditions"`
}

// MongoDBCluster is the Schema for the mongodbclusters API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
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
