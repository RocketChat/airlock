package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BackupScheduleConditionBackupCreateFailed         = "BackupCreateFailed"
	BackupScheduleConditionInternalTaskScheduleFailed = "InternalTaskScheduleFailed"
	BackupScheduleConditionReady                      = "Ready"
)

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

	// Suspend suspends the schedule if true
	// +kubebuilder:default=false
	Suspend *bool `json:"suspend,omitempty"`
}

// MongoDBBackupScheduleStatus defines the observed state of MongoDBBackupSchedule
// +kubebuilder:object:generate=true
// +k8s:deepcopy-gen=true
type MongoDBBackupScheduleStatus struct {
	// LastBackupTime is the time of the last successful backup
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// LastBackupName is the name of the last created backup
	LastBackupName string `json:"lastBackupName,omitempty"`

	// LastFailureTime is the time of the last failed backup
	LastFailureTime *metav1.Time `json:"lastFailureTime,omitempty"`

	// LastFailureMessage contains the error message from the last failed backup
	LastFailureMessage string `json:"lastFailureMessage,omitempty"`

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
