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
	"github.com/RocketChat/airlock/internal/conditions"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
)

type MongoDBRestoreReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Name      string
	statusMgr *conditions.ConditionsManager
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MongoDBRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	errors := internalerrors.New()
	defer measureControllerReconciliation(r.Name, start, errors)

	logger := log.FromContext(ctx)

	var restore airlockv1alpha1.MongoDBRestore
	err := r.Get(ctx, req.NamespacedName, &restore)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.Append(err)
	}

	r.statusMgr = conditions.NewManager(r, &restore.Status.Conditions, &restore, airlockv1alpha1.RestorePhaseRules)

	logger.Info("reconciling restore", "name", req.NamespacedName)

	if restore.Status.ObservedGeneration == nil {
		errors.Append(r.statusMgr.SetConditions(ctx, []conditions.Condition{
			{
				Type:    airlockv1alpha1.RestoreConditionBucketStoreReady,
				Status:  metav1.ConditionUnknown,
				Reason:  airlockv1alpha1.RestoreReasonRestoreNotStarted,
				Message: "Restore has not been started yet",
			},
			{
				Type:    airlockv1alpha1.RestoreConditionAccessRequestReady,
				Status:  metav1.ConditionUnknown,
				Reason:  airlockv1alpha1.RestoreReasonAccessRequestNotReady,
				Message: "Access request has not been started yet",
			},
		}))

		if errors.HasErrors() {
			return ctrl.Result{}, errors
		}
	}

	var store airlockv1alpha1.MongoDBBackupStore
	store.Name = restore.Spec.BackupStoreRef.Name
	if restore.Spec.BackupStoreRef.Namespace != "" {
		store.Namespace = restore.Spec.BackupStoreRef.Namespace
	} else {
		store.Namespace = restore.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(&store), &store); err != nil {
		return ctrl.Result{RequeueAfter: time.Minute}, errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.RestoreReasonBackupStoreNotFound, fmt.Sprintf("backup store not found: %s", err.Error()))).IfExists()
	}

	if !meta.IsStatusConditionTrue(store.Status.Conditions, airlockv1alpha1.StoreConditionBucketExists) {
		return ctrl.Result{RequeueAfter: time.Minute}, errors.Append(r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionBucketStoreReady, metav1.ConditionFalse, airlockv1alpha1.RestoreReasonBackupStoreNotFound, "backup store not ready")).IfExists()
	}

	if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionBucketStoreReady, metav1.ConditionTrue, airlockv1alpha1.RestoreReasonBackupStoreReady, "Backup store is ready"); err != nil {
		return ctrl.Result{}, errors.Append(err)
	}

	job, err := reconcileRestoreJob(ctx, r.Client, r.statusMgr, &restore)
	if err != nil {
		return ctrl.Result{}, errors.Append(err, r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionJobScheduled, metav1.ConditionFalse, airlockv1alpha1.RestoreReasonJobFailed, fmt.Sprintf("failed to reconcile restore job: %s", err.Error())))
	}

	if job.Status.Failed > 0 {
		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionJobFailed, metav1.ConditionTrue, "RestoreJobFailed", "Restore job has failed"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else if job.Status.Succeeded > 0 {
		restore.Status.CompletionTime = job.Status.CompletionTime
		restore.Status.Result = &airlockv1alpha1.MongoDBRestoreStatusResult{
			Job: &airlockv1alpha1.MongoDBRestoreStatusJob{
				ID:   string(job.GetUID()),
				Name: job.GetName(),
			},
		}
		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionJobCompleted, metav1.ConditionTrue, airlockv1alpha1.RestoreReasonJobCompleted, "Restore job has completed"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else {
		restore.Status.StartTime = job.Status.StartTime
		if err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.RestoreConditionJobScheduled, metav1.ConditionTrue, airlockv1alpha1.RestoreReasonJobScheduled, "Restore job has been scheduled"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *MongoDBRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBRestore{}).
		Owns(&batchv1.Job{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Owns(&airlockv1alpha1.MongoDBBackupStore{}).
		Complete(r)
}
