package metrics

import "time"

func UpdateBackupStore(namespace, name string, ready bool) {
	var v float64 = 0
	if ready {
		v = 1
	}

	BackupStoreTotal.WithLabelValues(namespace, name).Set(v)
}

func RemoveBackupStore(namespace, name string) {
	BackupStoreTotal.DeleteLabelValues(namespace, name)
}

func IncControllerError(controller string) {
	ControllerErrorCounter.WithLabelValues(controller).Inc()
}

func IncControllerSuccess(controller string) {
	ControllerSuccessCounter.WithLabelValues(controller).Inc()
}

func ObserveControllerReconcileDuration(controller string, duration time.Duration) {
	ControllerReconcileDuration.WithLabelValues(controller).Observe(duration.Seconds())
}

func IncBackupTotal(cluster, database, status string) {
	BackupTotal.WithLabelValues(cluster, database, status).Inc()
}

func SetBackupForPhase(namespace, name, cluster, database, schedule, phase string) {
	BackupTotal.WithLabelValues(namespace, name, cluster, database, schedule, phase).Set(1)
}

func RemoveBackupForPhase(namespace, name, cluster, database, schedule, phase string) {
	BackupTotal.DeleteLabelValues(namespace, name, cluster, database, schedule, phase)
}

func SetBackupScheduleGaugeForPhase(namespace, name, cluster, database, phase string) {
	BackupScheduleStatus.WithLabelValues(namespace, name, cluster, database, phase).Set(1)
}

// when a schedule is deleted, or phase changes, remove the gauge
func RemoveBackupScheduleGaugeForPhase(namespace, name, cluster, database, phase string) {
	BackupScheduleStatus.DeleteLabelValues(namespace, name, cluster, database, phase)
}

// always goes up
func IncBackupCreatedByScheduleGauge(namespace, name, cluster, database string) {
	BackupScheduleBackupCreatedTotal.WithLabelValues(namespace, name, cluster, database).Inc()
}

// if schedule is deleted
func RemoveBackupCreatedByScheduleGauge(namespace, name, cluster, database string) {
	BackupScheduleBackupCreatedTotal.DeleteLabelValues(namespace, name, cluster, database)
}
