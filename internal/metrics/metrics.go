package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	subsystemBackup = "airlock"
)

var (
	ControllerErrorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystemBackup,
			Name:      "controller_error_total",
			Help:      "Number of errors encountered by the controller",
		},
		[]string{"controller"},
	)

	ControllerSuccessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystemBackup,
			Name:      "controller_success_total",
			Help:      "Number of successful reconciliations by the controller",
		},
		[]string{"controller"},
	)

	ControllerReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: subsystemBackup,
			Name:      "controller_reconcile_duration_seconds",
			Help:      "Duration of controller reconciliations",
		},
		[]string{"controller"},
	)

	BackupTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystemBackup,
			Name:      "backup_total",
			Help:      "Number of MongoDBBackup resources",
		},
		// cluster = which MongoDBCluster resource this bbackup belongs to
		// database self explanatory
		[]string{"namespace", "name", "cluster", "database"},
	)

	BackupScheduleBackupCreatedTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystemBackup,
			Name:      "backup_schedule_backup_created_total",
			Help:      "Number of backups created by a MongoDBBackupSchedule",
		},
		[]string{"namespace", "name", "cluster", "database"}, // value is number of backups
	)

	BackupScheduleTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: subsystemBackup,
			Name:      "backup_schedule_total",
			Help:      "Number of MongoDBBackupSchedule resources",
		},
		[]string{"namespace", "name", "cluster", "database"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ControllerErrorCounter,
		ControllerSuccessCounter,
		ControllerReconcileDuration,
		BackupTotal,
		BackupScheduleBackupCreatedTotal,
		BackupScheduleTotal,
	)
}
