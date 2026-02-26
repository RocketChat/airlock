package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MongoDBRestoreControllerName = "MongoDBRestore"
)

// MongoDBRestoreSpec defines the desired state of MongoDBRestore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreSpec struct {
	Cluster                string `json:"cluster"`
	Database               string `json:"database"`
	DropDatabase           bool   `json:"dropDatabase,omitempty"`
	SourceBucketSecretName string `json:"sourceBucketSecretName"`
	Prefix                 string `json:"prefix,omitempty"`
}

// MongoDBRestoreStatus defines the observed state of MongoDBRestore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatus struct {
	StartTime      *metav1.Time                `json:"startTime,omitempty"`
	CompletionTime *metav1.Time                `json:"completionTime,omitempty"`
	Conditions     []metav1.Condition          `json:"conditions,omitempty"`
	Result         *MongoDBRestoreStatusResult `json:"result,omitempty"`
}

// MongoDBRestoreStatusResult defines the result of a restore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatusResult struct {
	JobRef *MongoDBRestoreStatusJobRef `json:"jobRef,omitempty"`
}

// MongoDBRestoreStatusJob references the restore job
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatusJobRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBRestoreSpec   `json:"spec,omitempty"`
	Status MongoDBRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBRestore{}, &MongoDBRestoreList{})
}
