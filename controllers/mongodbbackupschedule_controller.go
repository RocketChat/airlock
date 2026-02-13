package controllers

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/controllers/reconciler"
	"github.com/RocketChat/airlock/internal/conditions"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/RocketChat/airlock/internal/scheduler"
)

type MongoDBBackupScheduleReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler *scheduler.Scheduler

	name string

	statusMgr *conditions.StatusManager
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupschedules/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create

func (r *MongoDBBackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var schedule airlockv1alpha1.MongoDBBackupSchedule

	errors := internalerrors.New()

	start := time.Now()

	defer measureControllerReconciliation(r.name, start, errors)

	err := r.Get(ctx, req.NamespacedName, &schedule)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			// cr deleted
			log.Info("schedule deleted, removing all jobs", "namespacedName", req.NamespacedName)

			return ctrl.Result{}, errors.Append(r.Scheduler.RemoveJob(req.NamespacedName.String())).IfExists()
		}

		return ctrl.Result{}, errors.Append(err)
	}

	r.statusMgr = conditions.NewManager(r, &schedule.Status.Conditions, &schedule, airlockv1alpha1.BackupSchedulePhaseRules)

	if schedule.Status.ObservedGeneration == nil {
		log.Info("schedule is a fresh object, setting initial conditions", "schedule", schedule.Spec.Schedule)

		errors.Append(r.statusMgr.SetConditions(ctx, []conditions.Condition{
			{
				Type:    airlockv1alpha1.BackupScheduleConditionBucketStoreReady,
				Status:  metav1.ConditionUnknown,
				Reason:  "BucketStoreUnknown",
				Message: "Bucket store is unknown",
			},
			{
				Type:    airlockv1alpha1.BackupScheduleConditionBackupCreateFailed,
				Status:  metav1.ConditionUnknown,
				Reason:  "BackupCreationUnknown",
				Message: "Backup creation is unknown",
			},
			{
				Type:    airlockv1alpha1.BackupScheduleConditionInternalTaskScheduleFailed,
				Status:  metav1.ConditionUnknown,
				Reason:  "InternalTaskScheduleUnknown",
				Message: "Internal task schedule is unknown",
			},
		}))

		if errors.HasErrors() {
			return ctrl.Result{}, errors
		}
	}

	if schedule.DeletionTimestamp != nil && schedule.DeletionTimestamp.IsZero() {
		// being deleted
		r.handleDeletion(ctx, &schedule)

		if controllerutil.RemoveFinalizer(&schedule, airlockFinalizer) {
			if err := r.Update(ctx, &schedule); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	} else if schedule.DeletionTimestamp != nil && schedule.DeletionTimestamp.IsZero() {
		// add the finalizer
		if controllerutil.AddFinalizer(&schedule, airlockFinalizer) {
			if err := r.Update(ctx, &schedule); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	}

	defer measureBackupSchedulePhaseMetric(&schedule)

	log.Info("reconciling backup schedule", "schedule", schedule.Spec.Schedule)

	if schedule.Spec.Suspend != nil && *schedule.Spec.Suspend {
		// suspend == pending schedule, not failing

		log.Info("schedule is suspended, setting phase to pending, skipping further checks", "name", schedule.Name, "namespace", schedule.Namespace)

		schedule.Status.Phase = airlockv1alpha1.BackupSchedulePhasePending

		base := schedule.DeepCopy()

		errors.Append(r.Status().Patch(ctx, &schedule, client.MergeFrom(base)))

		return ctrl.Result{}, errors.IfExists()
	}

	var store airlockv1alpha1.MongoDBBackupStore
	store.Name = schedule.Spec.BackupSpec.BackupStoreRef.Name
	if schedule.Spec.BackupSpec.BackupStoreRef.Namespace != "" {
		store.Namespace = schedule.Spec.BackupSpec.BackupStoreRef.Namespace
	} else {
		store.Namespace = schedule.Namespace
	}

	log.Info("using store", "store", store.Name, "namespace", store.Namespace)

	if err := r.Get(ctx, client.ObjectKeyFromObject(&store), &store); err != nil {
		// we don't care if it's a notfound error this time
		errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.BackupScheduleReasonBackupStoreNotFound, fmt.Sprintf("backup store not found: %s", err.Error())))

		return ctrl.Result{}, errors.IfExists()
	}

	// store found, but not ready
	if meta.IsStatusConditionFalse(store.Status.Conditions, airlockv1alpha1.StoreConditionBucketExists) {
		log.Info("backup store is not ready", "store", store.Name, "namespace", store.Namespace)

		errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.BackupScheduleReasonBackupStoreNotReady, fmt.Sprintf("backup store is not ready: phase=%s", store.Status.Phase)))

		return ctrl.Result{}, errors.IfExists()
	}

	//s tore is ready
	if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionBucketStoreReady, metav1.ConditionTrue, "BackupStoreReady", "Backup store is ready"); err != nil {
		log.Error(err, "failed to set backup store ready condition", "store", store.Name, "namespace", store.Namespace)

		return ctrl.Result{}, errors.Append(err)
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

			errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionTrue, "InternalTaskScheduleFailed", fmt.Sprintf("Internal task schedule failed, schedule changed, failed to remove existing job, id: %s", existingJob.ID())))

			// we will not create another job if this fails
			return ctrl.Result{}, errors.IfExists()
		}
	}

	log.Info("scheduling new internal job for backup creation", "schedule", schedule.Spec.Schedule, "name", req.NamespacedName)

	// TODO(deb): add a flag to only keep x amount of backup crs and delete older ones
	job, err := r.Scheduler.AddJob(
		schedule.Spec.Schedule,
		req.NamespacedName.String(),
		func(ctx context.Context, params ...any) {
			r.reconcileBackupCr(ctx, params[0].(types.NamespacedName))
		},
		ctx,
		req.NamespacedName,
	)

	if err != nil {
		log.Error(err, "failed to create new job", "schedule", schedule.Spec.Schedule, "name", req.NamespacedName)

		errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionTrue, "InternalTaskScheduleFailed", "Internal task schedule failed, failed to create new job"))

		return ctrl.Result{}, errors.IfExists()
	}

	log.Info("new job scheduled")

	defer func() {
		if err := job.RunNow(); err != nil {
			log.Error(err, "failed to run job now", "job", job.ID())

			errors.Append(
				r.statusMgr.SetCondition(
					ctx,
					airlockv1alpha1.BackupScheduleConditionBackupCreateFailed,
					metav1.ConditionTrue,
					"BackupCreationFailed",
					fmt.Sprintf("Backup creation failed, failed to run job now, id: %s", job.ID()),
				),
			)

			return
		}

		errors.Append(
			r.statusMgr.SetCondition(
				ctx,
				airlockv1alpha1.BackupScheduleConditionBackupCreateFailed,
				metav1.ConditionFalse,
				"BackupCreationSucceeded",
				"Backup created successfully",
			),
		)
	}()

	return ctrl.Result{}, errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupScheduleConditionInternalTaskScheduleFailed, metav1.ConditionFalse, "InternalTaskScheduleSucceeded", "Internal task schedule succeeded")).IfExists()
}

func (r *MongoDBBackupScheduleReconciler) reconcileBackupCr(ctx context.Context, name types.NamespacedName) {
	errors := internalerrors.New()

	// these errors also contribute to schedule controller metrics
	defer measureControllerReconciliation(r.name, time.Now(), errors)

	log := log.FromContext(ctx)

	var schedule airlockv1alpha1.MongoDBBackupSchedule
	err := r.Get(ctx, name, &schedule)
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
	err = reconciler.Create(ctx, r.Client, &schedule, backup)

	if err != nil {
		errors.Append(err)

		log.Error(err, "failed to create or patch backup", "backup", backupName)

		if err := r.statusMgr.SetCondition(
			ctx,
			airlockv1alpha1.BackupScheduleConditionBackupCreateFailed,
			metav1.ConditionTrue,
			"BackupCreationFailed",
			fmt.Sprintf("failed to create backup: %s", err.Error()),
		); err != nil {
			errors.Append(err)

			log.Error(err, "failed to set backup creation failed condition", "backup", backupName)
		}

		return
	}

	log.Info("Created backup from schedule", "backup", backupName, "schedule", schedule.Name)

	if err := r.statusMgr.SetCondition(
		ctx,
		airlockv1alpha1.BackupScheduleConditionBackupCreateFailed,
		metav1.ConditionFalse,
		"BackupCreationSucceeded",
		"Backup created successfully",
	); err != nil {
		errors.Append(err)
		log.Error(err, "failed to set backup creation succeeded condition", "backup", backupName)
	}

	metrics.IncBackupCreatedByScheduleGauge(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database)
}

func (r *MongoDBBackupScheduleReconciler) handleDeletion(ctx context.Context, schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	log := log.FromContext(ctx)

	log.Info("deleting backup schedule", "schedule", schedule.Name)

	// remove all gauges for this schedule, even in case some stand around
	for _, phase := range airlockv1alpha1.BackupSchedulePhaseRules {
		metrics.RemoveBackupScheduleGaugeForPhase(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database, phase.Phase())
	}

	metrics.RemoveBackupCreatedByScheduleGauge(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database)
}

func measureBackupSchedulePhaseMetric(schedule *airlockv1alpha1.MongoDBBackupSchedule) {
	for _, phase := range airlockv1alpha1.BackupSchedulePhaseRules {
		if schedule.Status.Phase == phase.Phase() {
			metrics.SetBackupScheduleGaugeForPhase(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database, phase.Phase())
			continue
		}

		metrics.RemoveBackupScheduleGaugeForPhase(schedule.Namespace, schedule.Name, schedule.Spec.BackupSpec.Cluster, schedule.Spec.BackupSpec.Database, phase.Phase())
	}
}

func (r *MongoDBBackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error

	r.Scheduler, err = scheduler.NewScheduler()
	if err != nil {
		return err
	}

	r.Scheduler.Start()

	r.name = "MongoDBBackupSchedule"

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackupSchedule{}).
		Owns(&airlockv1alpha1.MongoDBBackup{}).
		Complete(r)
}
