package metrics

import "time"

func IncControllerError(controller string) {
	ControllerErrorCounter.WithLabelValues(controller).Inc()
}

func IncControllerSuccess(controller string) {
	ControllerSuccessCounter.WithLabelValues(controller).Inc()
}

func ObserveControllerReconcileDuration(controller string, duration time.Duration) {
	ControllerReconcileDuration.WithLabelValues(controller).Observe(duration.Seconds())
}

func SetBackupReady(namespace, name, cluster, database string) {
	BackupTotal.WithLabelValues(namespace, name, cluster, database).Set(1)
}

func SetBackupNotReady(namespace, name, cluster, database string) {
	BackupTotal.WithLabelValues(namespace, name, cluster, database).Set(0)
}

// cleanup
func RemoveBackupTotal(namespace, name, cluster, database string) {
	BackupTotal.DeleteLabelValues(namespace, name, cluster, database)
}

// cleanup
func RemoveBackupSchedule(namespace, name, cluster, database string) {
	BackupScheduleTotal.DeleteLabelValues(namespace, name, cluster, database)
}

func IncBackupCreatedBySchedule(namespace, name, cluster, database string) {
	BackupScheduleBackupCreatedTotal.WithLabelValues(namespace, name, cluster, database).Inc()
}

func SetBackupScheduleReady(namespace, name, cluster, database string) {
	BackupScheduleTotal.WithLabelValues(namespace, name, cluster, database).Set(1)
}

func SetBackupScheduleNotReady(namespace, name, cluster, database string) {
	BackupScheduleTotal.WithLabelValues(namespace, name, cluster, database).Set(0)
}
