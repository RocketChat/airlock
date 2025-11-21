package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MongoDBBackupSpec defines the desired state of MongoDBBackup
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupSpec struct {
	MongoDBRef MongoDBRef           `json:"mongodbRef"`
	Namespaces []MongoDBNamespace   `json:"namespaces,omitempty"`
	Storage    MongoDBBackupStorage `json:"storage"`
}

type MongoDBRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type MongoDBNamespace struct {
	Database    string   `json:"database"`
	Collections []string `json:"collections,omitempty"`
}

type MongoDBBackupStorage struct {
	Type string           `json:"type"`
	S3   *MongoDBBackupS3 `json:"s3,omitempty"`
}

type MongoDBBackupS3 struct {
	Endpoint  string      `json:"endpoint"`
	Bucket    string      `json:"bucket"`
	Region    string      `json:"region"`
	SecretRef S3SecretRef `json:"secretRef"`
	Prefix    string      `json:"prefix,omitempty"`
}

type S3SecretRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// MongoDBBackupStatus defines the observed state of MongoDBBackup
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupStatus struct {
	Phase          string             `json:"phase,omitempty"`
	StartTime      *metav1.Time       `json:"startTime,omitempty"`
	CompletionTime *metav1.Time       `json:"completionTime,omitempty"`
	BackupPath     string             `json:"backupPath,omitempty"`
	Size           string             `json:"size,omitempty"`
	Conditions     []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBBackupSpec   `json:"spec,omitempty"`
	Status MongoDBBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBBackup{}, &MongoDBBackupList{})
}
