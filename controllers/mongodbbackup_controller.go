package controllers

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/conditions"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
)

type MongoDBBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Name string

	statusMgr *conditions.StatusManager
}

const ()

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch,resourceNames=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MongoDBBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()

	errors := internalerrors.New()

	defer measureControllerReconciliation(r.Name, start, errors)

	log := log.FromContext(ctx)

	var backup airlockv1alpha1.MongoDBBackup

	err := r.Get(ctx, req.NamespacedName, &backup)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, errors.Append(err)
	}

	if backup.DeletionTimestamp != nil && backup.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(&backup, airlockFinalizer) {
			if err := r.Update(ctx, &backup); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}
	} else if backup.DeletionTimestamp != nil && backup.DeletionTimestamp.IsZero() {
		// being deleted
		r.handleDeletion(ctx, &backup)

		if controllerutil.RemoveFinalizer(&backup, airlockFinalizer) {
			if err := r.Update(ctx, &backup); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	}

	defer measureBackupMetrics(&backup)

	r.statusMgr = conditions.NewManager(r, &backup.Status.Conditions, &backup, airlockv1alpha1.BackupPhaseRules)

	log.Info("reconciling backup job", "name", req.NamespacedName)

	// first iteration
	if backup.Status.ObservedGeneration == nil {

		errors.Append(r.statusMgr.SetConditions(ctx, []conditions.Condition{
			{
				Type:    airlockv1alpha1.BackupConditionBucketStoreReady,
				Status:  metav1.ConditionUnknown,
				Reason:  airlockv1alpha1.BackupReasonBackupNotStarted,
				Message: "Backup has not been started yet",
			},
			{
				Type:    airlockv1alpha1.BackupConditionAccessRequestReady,
				Status:  metav1.ConditionUnknown,
				Reason:  airlockv1alpha1.BackupReasonAccessRequestNotReady,
				Message: "Access request has not been started yet",
			},
		}))

		if errors.HasErrors() {
			return ctrl.Result{}, errors
		}
	}

	// Check if backup store is ready
	var store airlockv1alpha1.MongoDBBackupStore

	store.Name = backup.Spec.BackupStoreRef.Name

	if backup.Spec.BackupStoreRef.Namespace != "" {
		store.Namespace = backup.Spec.BackupStoreRef.Namespace
	} else {
		store.Namespace = backup.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(&store), &store); err != nil {

		return ctrl.Result{RequeueAfter: time.Minute * 1}, errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.BackupReasonBackupStoreNotFound, fmt.Sprintf("backup store not found: %s", err.Error()))).IfExists()
	}

	// Check if backup store is ready
	if !meta.IsStatusConditionTrue(store.Status.Conditions, airlockv1alpha1.StoreConditionBucketExists) {
		return ctrl.Result{RequeueAfter: time.Minute * 1}, errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.BackupReasonBackupStoreNotFound, "backup store not found")).IfExists()
	}

	// store is ready
	if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionBucketStoreReady, metav1.ConditionTrue, airlockv1alpha1.BackupReasonBackupStoreReady, "Backup store is ready"); err != nil {
		return ctrl.Result{}, errors.Append(err)
	}

	job, err := reconcileBackupJob(ctx, r.Client, r.statusMgr, &backup)
	if err != nil {
		return ctrl.Result{}, errors.Append(err, r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionJobScheduled, metav1.ConditionFalse, airlockv1alpha1.BackupReasonJobFailed, fmt.Sprintf("failed to reconcile backup job: %s", err.Error())))
	}

	if job.Status.Failed > 0 {
		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionJobScheduled, metav1.ConditionFalse, "BackupJobFailed", "Backup job has failed"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else if job.Status.Succeeded > 0 {
		backup.Status.CompletionTime = job.Status.CompletionTime
		backup.Status.Result = &airlockv1alpha1.MongoDBBackupStatusResult{
			Path: "", // FIXME: add s3 path here for others polling
			Job: &airlockv1alpha1.MongoDBBackupStatusJob{
				ID:   string(job.GetUID()),
				Name: job.GetName(),
			},
		}

		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionJobCompleted, metav1.ConditionTrue, "BackupJobCompleted", "Backup job has completed"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else {
		// job hasn;t completed yet
		backup.Status.StartTime = job.Status.StartTime

		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionJobScheduled, metav1.ConditionTrue, "BackupJobScheduled", "Backup job has been scheduled"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	}

	// requeue after 5 seconds to check for completion
	// TODO: respond to job status changes
	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *MongoDBBackupReconciler) handleDeletion(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) {
	// just for metrics now.

	schedule := ""
	if backup.Labels["airlock.cloud.rocket.chat/scheduler"] != "" {
		schedule = backup.Labels["airlock.cloud.rocket.chat/scheduler"]
	}

	metrics.RemoveBackupForPhase(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database, schedule, backup.Status.Phase)
}

func measureBackupMetrics(backup *airlockv1alpha1.MongoDBBackup) {
	schedule := ""
	if backup.Labels["airlock.cloud.rocket.chat/scheduler"] != "" {
		schedule = backup.Labels["airlock.cloud.rocket.chat/scheduler"]
	}

	for _, phase := range airlockv1alpha1.BackupPhaseRules {
		if backup.Status.Phase == phase.Phase() {
			metrics.SetBackupForPhase(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database, schedule, phase.Phase())
			continue
		}

		metrics.RemoveBackupForPhase(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database, schedule, phase.Phase())
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Owns(&airlockv1alpha1.MongoDBBackupStore{}).
		Complete(r)
}
