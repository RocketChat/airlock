package v1alpha1

import (
	"github.com/RocketChat/airlock/internal/rules"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StoreConditionBucketExists = "BucketExists"

	MongoDBBackupStoreControllerName = "MongoDBBackupStore"

	StoreReasonBucketUnknown   = "BucketUnknown"
	StoreReasonBucketNotExists = "BucketNotExists"
	StoreReasonBucketExists    = "BucketExists"

	StorePhaseNotReady = "NotReady"
	StorePhaseReady    = "Ready"
)

var BackupStorePhaseRules = []rules.PhaseRule{
	rules.NewPhaseRule(
		// if bucket exists
		StorePhaseReady,
		rules.ConditionsAny(
			rules.ConditionEquals(StoreConditionBucketExists, metav1.ConditionTrue),
		),
	),
	// if bucket does not exist
	rules.NewPhaseRule(
		StorePhaseNotReady,
		rules.ConditionsAny(
			rules.ConditionEquals(StoreConditionBucketExists, metav1.ConditionFalse, metav1.ConditionUnknown),
		),
	),
}

// MongoDBBackupStoreSpec defines the desired state of MongoDBBackupStore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupStoreSpec struct {
	// +kubebuilder:validation:Enum=s3
	Type string                `json:"type"`
	S3   *MongoDBBackupStoreS3 `json:"s3,omitempty"`
}

type MongoDBBackupStoreS3 struct {
	Endpoint  string      `json:"endpoint"`
	Bucket    string      `json:"bucket"`
	Region    string      `json:"region"`
	SecretRef S3SecretRef `json:"secretRef"`
}

type S3SecretRef struct {
	Name     string           `json:"name"`
	Mappings S3SecretMappings `json:"mappings"`
}

type S3SecretMappings struct {
	AccessKeyID     ToKeyMap `json:"accessKeyId"`
	SecretAccessKey ToKeyMap `json:"secretAccessKey"`
}

type ToKeyMap struct {
	Key string `json:"key"`
}

// MongoDBBackupStoreStatus defines the observed state of MongoDBBackupStore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupStoreStatus struct {
	ObservedGeneration *int64             `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackupStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBBackupStoreSpec   `json:"spec,omitempty"`
	Status MongoDBBackupStoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackupStoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBBackupStore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBBackupStore{}, &MongoDBBackupStoreList{})
}

// implements Object2
func (o *MongoDBBackupStore) SetPhase(phase string) {
	o.Status.Phase = phase
}

func (o *MongoDBBackupStore) GetPhase() string {
	return o.Status.Phase
}

func (o *MongoDBBackupStore) SetObservedGeneration(generation int64) {
	o.Status.ObservedGeneration = &generation
}
