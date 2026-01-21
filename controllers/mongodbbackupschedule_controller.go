package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-co-op/gocron/v2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/controllers/reconciler"
)

type MongoDBBackupScheduleReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler gocron.Scheduler
}

// TODO: mor econsts
const (
	PhaseSucceeding = "Succeeding"
	PhaseFailing    = "Failing"
)

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create

func (r *MongoDBBackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var schedule airlockv1alpha1.MongoDBBackupSchedule

	schedule.Name = req.Name
	schedule.Namespace = req.Namespace

	err := r.Get(ctx, req.NamespacedName, &schedule)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := schedule.DeepCopy()

	log.Info("reconciling backup schedule", "schedule", schedule.Spec.Schedule)

	var store airlockv1alpha1.MongoDBBackupStore
	store.Name = schedule.Spec.BackupSpec.BackupStoreRef.Name
	if schedule.Spec.BackupSpec.BackupStoreRef.Namespace != "" {
		store.Namespace = schedule.Spec.BackupSpec.BackupStoreRef.Namespace
	} else {
		store.Namespace = schedule.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(&store), &store); err != nil {
		meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupStoreNotFound",
			Message: fmt.Sprintf("backup store not found: %s", err.Error()),
		})
		schedule.Status.Phase = PhaseFailing

		if err := r.Status().Patch(ctx, &schedule, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	if store.Status.Phase != "Ready" {
		meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupStoreNotReady",
			Message: fmt.Sprintf("backup store is not ready: phase=%s", store.Status.Phase),
		})
		schedule.Status.Phase = PhaseFailing

		if err := r.Status().Patch(ctx, &schedule, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	suspend := false
	if schedule.Spec.Suspend != nil {
		suspend = *schedule.Spec.Suspend
	}

	jobs := r.Scheduler.Jobs()
	var existingJob gocron.Job
	for _, job := range jobs {
		tags := job.Tags()
		if len(tags) >= 2 && tags[0] == schedule.Namespace && tags[1] == schedule.Name {
			existingJob = job
			break
		}
	}

	if suspend {
		if existingJob != nil {
			err = r.Scheduler.RemoveJob(existingJob.ID())
			if err != nil {
				log.Error(err, "failed to remove job")
			}
		}
		schedule.Status.Phase = PhaseFailing
		meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "Suspended",
			Message: "Schedule is suspended",
		})
	} else {
		if existingJob != nil {
			err = r.Scheduler.RemoveJob(existingJob.ID())
			if err != nil {
				log.Error(err, "failed to remove existing job")
			}
		}

		// TODO(deb): add a flag to only keep x amount of backup crs and delete older ones
		scheduleCopy := schedule.DeepCopy()
		_, err = r.Scheduler.NewJob(
			gocron.CronJob(schedule.Spec.Schedule, false),
			gocron.NewTask(
				func() {
					r.createBackup(context.Background(), scheduleCopy)
				},
			),
			gocron.WithTags(schedule.Namespace, schedule.Name),
		)

		if err != nil {
			meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "ScheduleCreationFailed",
				Message: fmt.Sprintf("failed to create schedule: %s", err.Error()),
			})
			schedule.Status.Phase = PhaseFailing

			if err := r.Status().Patch(ctx, &schedule, client.MergeFrom(base)); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	if err := r.updateStatusFromBackups(ctx, &schedule); err != nil {
		log.Error(err, "failed to update status from backups")
	}

	if err := r.Status().Patch(ctx, &schedule, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

func (r *MongoDBBackupScheduleReconciler) createBackup(ctx context.Context, schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	log := log.FromContext(ctx)

	timestamp := time.Now().Format("20060102150405")
	backupName := fmt.Sprintf("%s-%s", schedule.Name, timestamp)

	backup := &airlockv1alpha1.MongoDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: schedule.Namespace,
		},
	}

	backupSpec := schedule.Spec.BackupSpec
	backupSpec.Prefix = fmt.Sprintf("%s/%s", schedule.Spec.BackupSpec.Prefix, timestamp)

	_, err := reconciler.CreateOrPatch(ctx, r.Client, schedule, backup, func() error {
		backup.Spec = backupSpec
		if backup.Labels == nil {
			backup.Labels = make(map[string]string)
		}
		backup.Labels["airlock.cloud.rocket.chat/scheduler"] = schedule.Name
		return nil
	})

	if err != nil {
		log.Error(err, "failed to create or patch backup", "backup", backupName)
		return
	}

	log.Info("Created or patched backup from schedule", "backup", backupName, "schedule", schedule.Name)
}

func (r *MongoDBBackupScheduleReconciler) updateStatusFromBackups(ctx context.Context, schedule *airlockv1alpha1.MongoDBBackupSchedule) error {
	log := log.FromContext(ctx)

	var backupList airlockv1alpha1.MongoDBBackupList
	err := r.List(ctx, &backupList, client.InNamespace(schedule.Namespace), client.MatchingLabels{
		"airlock.cloud.rocket.chat/scheduler": schedule.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	var (
		activeBackups      []string
		lastBackupTime     *metav1.Time
		lastBackupName     string
		lastFailureTime    *metav1.Time
		lastFailureMessage string
	)

	recentSuccessCount := 0
	recentFailureCount := 0
	cutoffTime := time.Now().Add(-24 * time.Hour)

	for i := range backupList.Items {
		backup := &backupList.Items[i]

		if backup.Status.Phase != "Completed" && backup.Status.Phase != "Failed" {
			activeBackups = append(activeBackups, backup.Name)
		}

		if backup.Status.CompletionTime != nil {
			completionTime := backup.Status.CompletionTime.Time

			if completionTime.After(cutoffTime) {
				if backup.Status.Phase == "Completed" {
					recentSuccessCount++
					if lastBackupTime == nil || completionTime.After(lastBackupTime.Time) {
						lastBackupTime = backup.Status.CompletionTime
						lastBackupName = backup.Name
					}
				} else if backup.Status.Phase == "Failed" {
					recentFailureCount++
					if lastFailureTime == nil || completionTime.After(lastFailureTime.Time) {
						lastFailureTime = backup.Status.CompletionTime
						readyCondition := meta.FindStatusCondition(backup.Status.Conditions, "Ready")
						if readyCondition != nil {
							lastFailureMessage = readyCondition.Message
						}
					}
				}
			}
		}
	}

	if recentFailureCount > 0 && recentSuccessCount == 0 {
		schedule.Status.Phase = PhaseFailing
		meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "RecentBackupsFailed",
			Message: fmt.Sprintf("All recent backups failed (%d failures in last 24h)", recentFailureCount),
		})
	} else {
		schedule.Status.Phase = PhaseSucceeding
		meta.SetStatusCondition(&schedule.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "BackupsSucceeding",
			Message: fmt.Sprintf("Recent backups succeeding (%d successes, %d failures in last 24h)", recentSuccessCount, recentFailureCount),
		})
	}

	schedule.Status.ActiveBackups = activeBackups
	schedule.Status.LastBackupTime = lastBackupTime
	schedule.Status.LastBackupName = lastBackupName
	schedule.Status.LastFailureTime = lastFailureTime
	schedule.Status.LastFailureMessage = lastFailureMessage

	log.Info("updated schedule status", "phase", schedule.Status.Phase, "recentSuccesses", recentSuccessCount, "recentFailures", recentFailureCount)

	return nil
}

func (r *MongoDBBackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error

	r.Scheduler, err = gocron.NewScheduler()
	if err != nil {
		return err
	}

	r.Scheduler.Start()

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackupSchedule{}).
		Complete(r)
}
