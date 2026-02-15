package v1alpha1

import (
	"github.com/RocketChat/airlock/internal/rules"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MongoDBBackupControllerName = "MongoDBBackup"

	BackupConditionBucketStoreReady   = "BucketStoreReady"
	BackupConditionAccessRequestReady = "MongoDBAccessRequestReady"
	BackupConditionJobScheduled       = "JobScheduled"
	BackupConditionJobCompleted       = "JobCompleted"
	BackupConditionJobFailed          = "JobFailed"

	BackupPhasePending   = "Pending"
	BackupPhaseRunning   = "Running"
	BackupPhaseCompleted = "Completed"
	BackupPhaseFailed    = "Failed"

	BackupReasonBackupNotStarted = "BackupNotStarted"

	BackupReasonBackupStoreNotFound   = "BackupStoreNotFound"
	BackupReasonAccessRequestNotFound = "AccessRequestNotFound"
	BackupReasonAccessRequestNotReady = "AccessRequestNotReady"
	BackupReasonAccessRequestReady    = "AccessRequestReady"

	BackupReasonBackupStoreReady = "BackupStoreReady"
	BackupReasonJobScheduled     = "JobScheduled"
	BackupReasonJobCompleted     = "JobCompleted"
	BackupReasonJobFailed        = "JobFailed"
)

var BackupPhaseRules = []rules.PhaseRule{
	// a successful backup
	rules.NewPhaseRule(
		BackupPhaseCompleted,
		rules.ConditionsAll(
			// store is ready
			rules.ConditionEquals(BackupConditionBucketStoreReady, metav1.ConditionTrue),
			// accessrequest is ready
			rules.ConditionEquals(BackupConditionAccessRequestReady, metav1.ConditionTrue),
			// job is scheduled
			rules.ConditionEquals(BackupConditionJobCompleted, metav1.ConditionTrue),
		),
	),
	// for a running backup, check that consumes most conditions first
	rules.NewPhaseRule(
		BackupPhaseRunning,
		rules.ConditionsAll(
			// store is ready
			rules.ConditionEquals(BackupConditionBucketStoreReady, metav1.ConditionTrue),
			// accessrequest is ready
			rules.ConditionEquals(BackupConditionAccessRequestReady, metav1.ConditionTrue),
			// job is scheduled
			rules.ConditionEquals(BackupConditionJobScheduled, metav1.ConditionTrue),
		),
	),
	// a failed backup can consist of any of the conditions being false-y
	rules.NewPhaseRule(
		BackupPhaseFailed,
		rules.ConditionsAny(
			// store is not ready
			rules.ConditionEquals(BackupConditionBucketStoreReady, metav1.ConditionFalse),
			// accessrequest is not ready
			rules.ConditionEquals(BackupConditionAccessRequestReady, metav1.ConditionFalse),
			// job is failed
			rules.ConditionEquals(BackupConditionJobFailed, metav1.ConditionTrue),
			// job schedule failed
			// rules.ConditionEquals(BackupConditionJobScheduled, metav1.ConditionFalse),
		),
	),
	// pending backup is an amalgamation of all the conditions that are not yet met
	// when any of the precursors are in unknown state
	rules.NewPhaseRule(
		BackupPhasePending,
		rules.ConditionsAny(
			// store is in unknown state
			rules.ConditionEquals(BackupConditionBucketStoreReady, metav1.ConditionUnknown),
			// accessrequest is in unknown state
			rules.ConditionEquals(BackupConditionAccessRequestReady, metav1.ConditionUnknown),
		),
	),
	rules.NewPhaseRule(
		BackupPhasePending,
		rules.ConditionsAll(
			// store is either ready or hasn't been checked yet by their controller
			rules.ConditionEquals(BackupConditionBucketStoreReady, metav1.ConditionTrue, metav1.ConditionUnknown),
			// accessrequest is either ready or hasn't been checked yet by access requyest controller
			rules.ConditionEquals(BackupConditionAccessRequestReady, metav1.ConditionTrue, metav1.ConditionUnknown),
			// job is not scheduled yet
			rules.ConditionEquals(BackupConditionJobScheduled, metav1.ConditionUnknown, metav1.ConditionFalse),
		),
	),
}

// MongoDBBackupSpec defines the desired state of MongoDBBackup
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupSpec struct {
	Cluster             string                  `json:"cluster"`
	Database            string                  `json:"database"`
	ExcludedCollections []string                `json:"excludedCollections,omitempty"`
	IncludedCollections []string                `json:"includedCollections,omitempty"`
	BackupStoreRef      MongoDBBackupStoreRef   `json:"backupStoreRef"`
	Prefix              string                  `json:"prefix,omitempty"`
	Encrypt             MongoDBBackupEncryption `json:"encrypt,omitempty"`
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

type MongoDBEncryptionAgeSecretRef struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Mapping   ToKeyMap `json:"mapping"`
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
	Phase              string             `json:"phase,omitempty"`
	ObservedGeneration *int64             `json:"observedGeneration,omitempty"`
	StartTime          *metav1.Time       `json:"startTime,omitempty"`
	CompletionTime     *metav1.Time       `json:"completionTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`

	Result *MongoDBBackupStatusResult `json:"result,omitempty"`
}

type MongoDBBackupStatusResult struct {
	Path string                  `json:"path,omitempty"`
	Job  *MongoDBBackupStatusJob `json:"job,omitempty"`
}

type MongoDBBackupStatusJob struct {
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

func (o *MongoDBBackup) SetPhase(phase string) {
	o.Status.Phase = phase
}

func (o *MongoDBBackup) GetPhase() string {
	return o.Status.Phase
}

func (o *MongoDBBackup) SetObservedGeneration(generation int64) {
	o.Status.ObservedGeneration = &generation
}
