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
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

// TODO(deb): use more consts for phasesm, reasons etc

type MongoDBBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	StatusBackupCompleted = "Completed"
	StatusBackupFailed    = "Failed"
	StatusBackupPending   = "Pending"
)

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch,resourceNames=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MongoDBBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var backup airlockv1alpha1.MongoDBBackup

	backup.Name = req.Name
	backup.Namespace = req.Namespace

	err := r.Get(ctx, req.NamespacedName, &backup)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := backup.DeepCopy()

	log.Info("reconciling backup job")

	// Check if backup store is ready
	var store airlockv1alpha1.MongoDBBackupStore
	store.Name = backup.Spec.BackupStoreRef.Name
	if backup.Spec.BackupStoreRef.Namespace != "" {
		store.Namespace = backup.Spec.BackupStoreRef.Namespace
	} else {
		store.Namespace = backup.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(&store), &store); err != nil {
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupStoreNotFound",
			Message: fmt.Sprintf("backup store not found: %s", err.Error()),
		})
		backup.Status.Phase = StatusBackupFailed

		if err := r.Status().Patch(ctx, &backup, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// Check if backup store is ready
	if store.Status.Phase != "Ready" {
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupStoreNotReady",
			Message: fmt.Sprintf("backup store is not ready: phase=%s", store.Status.Phase),
		})
		if backup.Status.Phase == "" {
			backup.Status.Phase = StatusBackupPending
		}

		if err := r.Status().Patch(ctx, &backup, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// Initialize phase if not set
	if backup.Status.Phase == "" {
		backup.Status.Phase = StatusBackupPending
		if backup.Status.StartTime == nil {
			now := metav1.Now()
			backup.Status.StartTime = &now
		}
	}

	job, err := reconcileBackupJob(ctx, r.Client, &backup)
	if err != nil {
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "FailedJobReconciliation",
			Message: fmt.Sprintf("failed to reconcile backup job: %s", err.Error()),
		})
		backup.Status.Phase = StatusBackupFailed

		if err := r.Status().Patch(ctx, &backup, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// Update phase based on job status
	if job.Status.CompletionTime != nil {
		backup.Status.Phase = StatusBackupCompleted
		backup.Status.CompletionTime = job.Status.CompletionTime
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "BackupCompleted",
			Message: "Backup job completed successfully",
		})
	} else if job.Status.Failed > 0 {
		backup.Status.Phase = StatusBackupFailed
		backup.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupFailed",
			Message: "Backup job failed",
		})
	} else if backup.Status.Phase == StatusBackupPending {
		backup.Status.Phase = StatusBackupPending
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "BackupInProgress",
			Message: "Backup job is in progress",
		})
	}

	if err := r.Status().Patch(ctx, &backup, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	// If not completed or failed, requeue to check status
	if backup.Status.Phase == StatusBackupPending {
		return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
	}

	return ctrl.Result{}, nil
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
