package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DestinationBucketSecretRefAccessKeyID     = "accessKeyId"
	DestinationBucketSecretRefSecretAccessKey = "secretAccessKey"
	DestinationBucketSecretRefRegion          = "region"
	DestinationBucketSecretRefBucket          = "bucket"
)

// MongoDBBackupSpec defines the desired state of MongoDBBackup
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupSpec struct {
	Cluster             string                  `json:"cluster"`
	Database            string                  `json:"database"`
	ExcludedCollections []string                `json:"excludedCollections,omitempty"`
	Prefix              string                  `json:"prefix,omitempty"`
	Encryption          MongoDBBackupEncryption `json:"encryption,omitempty"`

	DestinationBucketSecretRef MongoDBDestinationBucketSecretRef `json:"destinationBucketSecretRef"`
}

// bucket
// region
// accessKeyId
// secretAccessKey
type MongoDBDestinationBucketSecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type MongoDBBackupStoreRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type MongoDBBackupEncryption struct {
	Enabled bool `json:"enabled"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=age
	// +kubebuilder:validation:Enum=age;
	Engine       string                        `json:"engine,omitempty"` // currently only supported engine is age
	AgeSecretRef MongoDBEncryptionAgeSecretRef `json:"ageSecretRef"`
}

// keys
type MongoDBEncryptionAgeSecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// type MongoDBBackupS3 struct {
// 	Endpoint  string      `json:"endpoint"`
// 	Bucket    string      `json:"bucket"`
// 	Region    string      `json:"region"`
// 	SecretRef S3SecretRef `json:"secretRef"`
// 	Prefix    string      `json:"prefix,omitempty"`
// }

// MongoDBBackupStatus defines the observed state of MongoDBBackup
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupStatus struct {
	StartTime      *metav1.Time       `json:"startTime,omitempty"`
	CompletionTime *metav1.Time       `json:"completionTime,omitempty"`
	Conditions     []metav1.Condition `json:"conditions,omitempty"`

	Result *MongoDBBackupStatusResult `json:"result,omitempty"`
}

type MongoDBBackupStatusResult struct {
	JobRef *MongoDBBackupStatusJobRef `json:"jobRef,omitempty"`
}

type MongoDBBackupStatusJobRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
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
