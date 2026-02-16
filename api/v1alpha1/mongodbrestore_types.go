package v1alpha1

import (
	"github.com/RocketChat/airlock/internal/rules"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MongoDBRestoreControllerName = "MongoDBRestore"

	RestoreConditionBucketStoreReady   = "BucketStoreReady"
	RestoreConditionAccessRequestReady = "MongoDBAccessRequestReady"
	RestoreConditionJobScheduled       = "JobScheduled"
	RestoreConditionJobCompleted       = "JobCompleted"
	RestoreConditionJobFailed          = "JobFailed"

	RestorePhasePending   = "Pending"
	RestorePhaseRunning   = "Running"
	RestorePhaseCompleted = "Completed"
	RestorePhaseFailed    = "Failed"

	RestoreReasonRestoreNotStarted = "RestoreNotStarted"

	RestoreReasonBackupStoreNotFound   = "BackupStoreNotFound"
	RestoreReasonAccessRequestNotFound = "AccessRequestNotFound"
	RestoreReasonAccessRequestNotReady = "AccessRequestNotReady"
	RestoreReasonAccessRequestReady    = "AccessRequestReady"

	RestoreReasonBackupStoreReady = "BackupStoreReady"
	RestoreReasonJobScheduled     = "JobScheduled"
	RestoreReasonJobCompleted     = "JobCompleted"
	RestoreReasonJobFailed        = "JobFailed"
)

var RestorePhaseRules = []rules.PhaseRule{
	rules.NewPhaseRule(
		RestorePhaseFailed,
		rules.ConditionsAny(
			rules.ConditionEquals(RestoreConditionJobFailed, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionJobCompleted, metav1.ConditionFalse),
			rules.ConditionEquals(RestoreConditionJobScheduled, metav1.ConditionFalse),
		),
	),
	rules.NewPhaseRule(
		RestorePhasePending,
		rules.ConditionsAny(
			rules.ConditionEquals(RestoreConditionBucketStoreReady, metav1.ConditionUnknown, metav1.ConditionFalse),
			rules.ConditionEquals(RestoreConditionAccessRequestReady, metav1.ConditionUnknown, metav1.ConditionFalse),
		),
	),
	rules.NewPhaseRule(
		RestorePhaseRunning,
		rules.ConditionsAll(
			rules.ConditionEquals(RestoreConditionBucketStoreReady, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionAccessRequestReady, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionJobScheduled, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionJobCompleted, metav1.ConditionUnknown),
		),
	),
	rules.NewPhaseRule(
		RestorePhaseCompleted,
		rules.ConditionsAll(
			rules.ConditionEquals(RestoreConditionBucketStoreReady, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionAccessRequestReady, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionJobScheduled, metav1.ConditionTrue),
			rules.ConditionEquals(RestoreConditionJobCompleted, metav1.ConditionTrue),
		),
	),
}

// MongoDBRestoreSpec defines the desired state of MongoDBRestore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreSpec struct {
	Cluster        string                `json:"cluster"`
	Database       string                `json:"database"`
	DropDatabase   bool                  `json:"dropDatabase,omitempty"`
	S3Path         string                `json:"s3Path"`
	BackupStoreRef MongoDBBackupStoreRef `json:"backupStoreRef"`
}

// MongoDBRestoreStatus defines the observed state of MongoDBRestore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatus struct {
	Phase              string                      `json:"phase,omitempty"`
	ObservedGeneration *int64                      `json:"observedGeneration,omitempty"`
	StartTime          *metav1.Time                `json:"startTime,omitempty"`
	CompletionTime     *metav1.Time                `json:"completionTime,omitempty"`
	Conditions         []metav1.Condition          `json:"conditions,omitempty"`
	Result             *MongoDBRestoreStatusResult `json:"result,omitempty"`
}

// MongoDBRestoreStatusResult defines the result of a restore
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatusResult struct {
	Job *MongoDBRestoreStatusJob `json:"job,omitempty"`
}

// MongoDBRestoreStatusJob references the restore job
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBRestoreStatusJob struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
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

func (o *MongoDBRestore) SetPhase(phase string) {
	o.Status.Phase = phase
}

func (o *MongoDBRestore) GetPhase() string {
	return o.Status.Phase
}

func (o *MongoDBRestore) SetObservedGeneration(generation int64) {
	o.Status.ObservedGeneration = &generation
}
