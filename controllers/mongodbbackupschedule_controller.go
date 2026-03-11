package controllers

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/RocketChat/airlock/internal/scheduler"
	"github.com/RocketChat/airlock/pkg/conditions"
	"github.com/RocketChat/airlock/pkg/reconciler"
	"github.com/RocketChat/airlock/pkg/webhook"
)

const (
	EventReasonBackupScheduleSuspended       = "BackupScheduleSuspended"
	EventReasonInternalTaskScheduleFailed    = "InternalTaskScheduleFailed"
	EventReasonInternalTaskScheduleSucceeded = "InternalTaskScheduleSucceeded"
	EventReasonBackupCreationFailed          = "BackupCreationFailed"
	EventReasonBackupCreationSucceeded       = "BackupCreationSucceeded"
)

type MongoDBBackupScheduleReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler *scheduler.Scheduler

	name string

	recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create

func (r *MongoDBBackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	schedule := &airlockv1alpha1.MongoDBBackupSchedule{}

	errors := internalerrors.New()

	defer measureControllerReconciliation(r.name, time.Now(), errors)

	err := r.Get(ctx, req.NamespacedName, schedule)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			// cr deleted
			log.Info("schedule deleted, removing all jobs", "namespacedName", req.NamespacedName)

			return ctrl.Result{}, errors.Append(r.Scheduler.RemoveJob(req.NamespacedName.String())).IfExists()
		}

		return ctrl.Result{}, errors.Append(err)
	}

	webhookMgr, err := webhook.ParseAnnotations(schedule.Annotations)
	if err != nil {
		return ctrl.Result{}, errors.Append(err)
	}

	statusMgr := conditions.NewManager(r.Client, schedule, &schedule.Status.Conditions, webhookMgr)

	if schedule.DeletionTimestamp != nil && schedule.DeletionTimestamp.IsZero() {
		// being deleted
		r.handleDeletion(ctx, schedule)

		if controllerutil.RemoveFinalizer(schedule, airlockFinalizer) {
			if err := r.Update(ctx, schedule); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	} else if schedule.DeletionTimestamp != nil && schedule.DeletionTimestamp.IsZero() {
		// add the finalizer
		if controllerutil.AddFinalizer(schedule, airlockFinalizer) {
			if err := r.Update(ctx, schedule); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	}

	log.Info("reconciling backup schedule", "schedule", schedule.Spec.Schedule)

	if schedule.Spec.Suspend != nil && *schedule.Spec.Suspend {
		log.Info("schedule is suspended, skipping further checks")

		changed, err := statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonBackupScheduleSuspended, "Schedule is suspended")

		if changed {
			r.measureBackupScheduleNotReady(schedule)
		}

		return ctrl.Result{}, errors.Append(err).IfExists()
	}

	existingJob := r.Scheduler.GetJob(req.NamespacedName.String())

	if existingJob != nil {
		// not suspended, but same schedule, no need to reconcile
		if scheduler.IsSameSchedule(existingJob, schedule.Spec.Schedule) {
			log.Info("schedule is the same, no need to reconcile", "schedule", schedule.Spec.Schedule, "name", req.NamespacedName)

			return ctrl.Result{}, nil
		}

		// remove existing job before reconciling
		log.Info("removing existing job schedule changed", "job", existingJob.ID(), "name", req.NamespacedName)

		err = r.Scheduler.RemoveJob(req.NamespacedName.String())
		if err != nil {
			log.Error(err, "failed to remove existing job", "job", existingJob.ID())

			errors.Append(err)

			message := fmt.Sprintf("Internal task schedule failed, schedule changed, failed to remove existing job, id: %s", existingJob.ID())

			changed, err := statusMgr.SetCondition(ctx,
				airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonInternalTaskScheduleFailed, message)

			if changed {
				r.measureBackupScheduleNotReady(schedule)
			}

			// we will not create another job if this fails
			return ctrl.Result{}, errors.Append(err)
		}
	}

	log.Info("scheduling new internal job for backup creation")

	job, err := r.Scheduler.AddJob(
		schedule.Spec.Schedule,
		req.NamespacedName.String(),
		func(ctx context.Context, params ...any) {
			r.reconcileBackup(ctx, params[0].(types.NamespacedName))
		},
		ctx,
		req.NamespacedName,
	)

	if err != nil {
		log.Error(err, "failed to create new job", "schedule", schedule.Spec.Schedule, "name", req.NamespacedName)

		errors.Append(err)

		message := fmt.Sprintf("Internal task schedule failed, failed to create new job")

		changed, err := statusMgr.SetCondition(ctx,
			airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonInternalTaskScheduleFailed, message)

		if changed {
			r.measureBackupScheduleNotReady(schedule)
		}

		return ctrl.Result{}, errors.Append(err)
	}

	log.Info("new job scheduled")

	defer func() {
		if err := job.RunNow(); err != nil {
			log.Error(err, "failed to run job now", "jobID", job.ID())

			message := fmt.Sprintf("Backup creation failed, failed to run job now, id: %s", job.ID())

			changed, err := statusMgr.SetCondition(ctx,
				airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonBackupCreationFailed, message)

			if changed {
				r.measureBackupScheduleNotReady(schedule)
			}

			log.Error(err, "failed to run first job", "jobID", job.ID())

			return
		}

		changed, err := statusMgr.SetCondition(ctx,
			airlockv1alpha1.ConditionReady, metav1.ConditionTrue, EventReasonBackupCreationSucceeded, "Backup creation succeeded")
		if err != nil {
			log.Error(err, "failed to set backup creation succeeded condition", "jobID", job.ID())
			return
		}

		if changed {
			r.measureBackupScheduleReady(schedule)
		}
	}()

	if _, err := statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, EventReasonInternalTaskScheduleSucceeded, "Internal task schedule succeeded"); err != nil {
		return ctrl.Result{}, errors.Append(err)
	}

	return ctrl.Result{}, nil
}

func (r *MongoDBBackupScheduleReconciler) reconcileBackup(ctx context.Context, name types.NamespacedName) {
	errors := internalerrors.New()

	// these errors also contribute to schedule controller metrics
	defer measureControllerReconciliation(r.name, time.Now(), errors)

	log := log.FromContext(ctx)

	schedule := &airlockv1alpha1.MongoDBBackupSchedule{}
	err := r.Get(ctx, name, schedule)
	if err != nil {
		log.Error(err, "failed to get schedule", "name", name)

		errors.Append(err)

		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "schedule deleted, skipping backup creation, deleting internal job", "name", name)

			if err := r.Scheduler.RemoveJob(name.String()); err != nil {
				log.Error(err, "failed to delete internal job", "name", name)

				errors.Append(err)
			}
		}

		return
	}

	webhookMgr, err := webhook.ParseAnnotations(schedule.Annotations)
	if err != nil {
		log.Error(err, "failed to parse webhook annotations", "name", schedule.Name)
		return
	}

	statusMgr := conditions.NewManager(r.Client, schedule, &schedule.Status.Conditions, webhookMgr)
	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, schedule)

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

	backup.Spec = backupSpec

	if backup.Labels == nil {
		backup.Labels = make(map[string]string)
	}

	backup.Labels["airlock.cloud.rocket.chat/scheduler"] = schedule.Name

	// timestamp based, can not exist the same here, or we have a problem somewhere else
	err = reconciler.Create(ctx, backup, reconcilerOpts)
	if err != nil {
		errors.Append(err)
		log.Error(err, "failed to create backup", "backupName", backupName)
		message := fmt.Sprintf("failed to create backup: %s", err.Error())

		changed, err := statusMgr.SetCondition(ctx,
			airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonBackupCreationFailed, message)

		if err != nil {
			errors.Append(err)

			log.Error(err, "failed to set backup creation failed condition", "backup", backupName)
		}

		if changed {
			r.measureBackupScheduleNotReady(schedule)
		}

		return
	}

	log.Info("Created backup from schedule", "backupName", backupName)

	message := "Backup created successfully"

	changed, err := statusMgr.SetCondition(ctx,
		airlockv1alpha1.ConditionReady, metav1.ConditionTrue, EventReasonBackupCreationSucceeded, message)

	if err != nil {
		errors.Append(err)
		log.Error(err, "failed to set backup creation succeeded condition", "backup", backupName)
	}

	if changed {
		r.measureBackupScheduleReady(schedule)
	}
}

func (r *MongoDBBackupScheduleReconciler) handleDeletion(ctx context.Context, schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	log := log.FromContext(ctx)

	log.Info("deleting backup schedule", "schedule", schedule.Name)

	// remove all gauges for this schedule, even in case some stand around
	metrics.RemoveBackupSchedule(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database)
}

func (r *MongoDBBackupScheduleReconciler) measureBackupScheduleReady(schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	metrics.SetBackupScheduleReady(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database)
}

func (r *MongoDBBackupScheduleReconciler) measureBackupScheduleNotReady(schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	metrics.SetBackupScheduleNotReady(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database)
}

func (r *MongoDBBackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error

	r.Scheduler, err = scheduler.NewScheduler()
	if err != nil {
		return err
	}

	r.Scheduler.Start()

	r.recorder = mgr.GetEventRecorderFor("airlock")

	r.name = "MongoDBBackupSchedule"

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackupSchedule{}).
		Owns(&airlockv1alpha1.MongoDBBackup{}).
		Complete(r)
}
