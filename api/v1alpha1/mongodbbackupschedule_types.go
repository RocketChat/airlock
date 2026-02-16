package v1alpha1

import (
	"github.com/RocketChat/airlock/internal/rules"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BackupSchedulePhaseRunning = "Running"
	BackupSchedulePhasePending = "Pending"
	BackupSchedulePhaseFailed  = "Failed"

	BackupScheduleConditionBucketStoreReady           = "BucketStoreReady"
	BackupScheduleConditionBackupCreateFailed         = "BackupCreateFailed"
	BackupScheduleConditionInternalTaskScheduleFailed = "InternalTaskScheduleFailed"

	BackupScheduleReasonBackupStoreNotFound = "BackupStoreNotFound"
	BackupScheduleReasonBackupStoreNotReady = "BackupStoreNotReady"
	BackupScheduleReasonBackupCreated       = "BackupCreated"
)

var BackupSchedulePhaseRules = []rules.PhaseRule{
	rules.NewPhaseRule(
		BackupSchedulePhaseRunning,
		// store must be ready
		// backup creation did not fail
		// schedule must not be suspended
		rules.ConditionsAll(
			rules.ConditionEquals(BackupScheduleConditionBucketStoreReady, metav1.ConditionTrue),
			rules.ConditionEquals(BackupScheduleConditionBackupCreateFailed, metav1.ConditionFalse),
			rules.ConditionEquals(BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionFalse),
		),
	),
	rules.NewPhaseRule(
		BackupSchedulePhaseFailed,
		// backup creation failed
		// or internal task schedule failed
		rules.ConditionsAny(
			rules.ConditionEquals(BackupScheduleConditionBackupCreateFailed, metav1.ConditionTrue),
			rules.ConditionEquals(BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionTrue),
		),
	),
	rules.NewPhaseRule(
		BackupSchedulePhasePending,
		// store may niot be ready yet, keep itr in pending state
		// backup creation unknown, pending is ok
		// suspended, pending
		rules.ConditionsAny(
			rules.ConditionEquals(BackupScheduleConditionBucketStoreReady, metav1.ConditionUnknown, metav1.ConditionFalse),
			rules.ConditionEquals(BackupScheduleConditionBackupCreateFailed, metav1.ConditionUnknown),
			rules.ConditionEquals(BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionUnknown),
		),
	),
}

// MongoDBBackupScheduleSpec defines the desired state of MongoDBBackupSchedule
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupScheduleSpec struct {
	// Schedule is a cron expression defining when backups should run
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// BackupSpec defines the template for creating MongoDBBackup resources
	// +kubebuilder:validation:Required
	BackupSpec MongoDBBackupSpec `json:"backupSpec"`

	// SuccessfulJobsHistoryLimit is the number of successful backup jobs to keep
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit is the number of failed backup jobs to keep
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Suspend suspends the schedule if true
	// +kubebuilder:default=false
	Suspend *bool `json:"suspend,omitempty"`
}

// MongoDBBackupScheduleStatus defines the observed state of MongoDBBackupSchedule
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupScheduleStatus struct {
	// Phase indicates the overall status of the schedule
	// +kubebuilder:validation:Enum=Running;Pending;Failed;
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration is the generation of the schedule that was last processed by the controller
	// +kubebuilder:validation:Format=int64
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// LastBackupTime is the time of the last successful backup
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// LastBackupName is the name of the last created backup
	LastBackupName string `json:"lastBackupName,omitempty"`

	// LastFailureTime is the time of the last failed backup
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`

	// LastFailureMessage contains the error message from the last failed backup
	LastFailureMessage string `json:"lastFailureMessage,omitempty"`

	// ActiveBackups is a list of active backup names created by this schedule
	ActiveBackups []string `json:"activeBackups,omitempty"`

	// Conditions represent the latest available observations of the schedule's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Last Backup",type="date",JSONPath=".status.lastBackupTime"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackupSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBBackupScheduleSpec   `json:"spec,omitempty"`
	Status MongoDBBackupScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MongoDBBackupScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBBackupSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBBackupSchedule{}, &MongoDBBackupScheduleList{})
}

func (o *MongoDBBackupSchedule) SetPhase(phase string) {
	o.Status.Phase = phase
}

func (o *MongoDBBackupSchedule) GetPhase() string {
	return o.Status.Phase
}

func (o *MongoDBBackupSchedule) SetObservedGeneration(generation int64) {
	o.Status.ObservedGeneration = &generation
}
