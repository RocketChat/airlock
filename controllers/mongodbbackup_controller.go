package controllers

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/config"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/RocketChat/airlock/pkg/conditions"
	"github.com/RocketChat/airlock/pkg/portmaster"
	"github.com/RocketChat/airlock/pkg/webhook"
)

const (
	EventReasonDestinationBucketInvalid        = "DestinationBucketInvalid"
	EventReasonAccessRequestNotReady           = "MongoDBAccessRequestNotReady"
	EventReasonDatabaseSizeUnknown             = "DatabaseSizeUnknown"
	ReasonPersistentVolumeClaimReconcileFailed = "PersistentVolumeClaimReconcileFailed"
	ReasonBackupJobScheduleFailed              = "BackupJobScheduleFailed"
	ReasonBackupJobCompleted                   = "BackupJobCompletedSuccessfully"
	ReasonBackupJobFailed                      = "BackupJobFailed"
	ReasonBackupJobScheduled                   = "BackupJobScheduledSuccessfully"
)

type MongoDBBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	name string

	Config *config.Config

	statusMgr *conditions.ConditionsManager

	recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch,resourceNames=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=storageclasses,verbs=get;list;watch;

func (r *MongoDBBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()

	errors := internalerrors.New()

	defer measureControllerReconciliation(r.name, start, errors)

	backup := &airlockv1alpha1.MongoDBBackup{}

	err := r.Get(ctx, req.NamespacedName, backup)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.FromContext(ctx)

	if backup.DeletionTimestamp != nil && backup.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(backup, airlockFinalizer) {
			if err := r.Update(ctx, backup); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}
	} else if backup.DeletionTimestamp != nil && !backup.DeletionTimestamp.IsZero() {
		r.handleDeletion(ctx, backup)

		if controllerutil.RemoveFinalizer(backup, airlockFinalizer) {
			if err := r.Update(ctx, backup); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	}

	webhookMgr, err := webhook.ParseAnnotations(backup.Annotations)
	if err != nil {
		return ctrl.Result{}, errors.Append(err)
	}

	r.statusMgr = conditions.NewManager(r.Client, backup, &backup.Status.Conditions, webhookMgr)

	if r.statusMgr.IsConditionTrueAndValid(airlockv1alpha1.ConditionReady) {
		// spec did not change, job has already finished
		// we don't care why this iteration got triggered.
		logger.Info("backup job has already finished, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	runner := portmaster.NewPortmasterRunner(
		ctx,
		portmaster.PortmasterModeExportDatabase,
		portmaster.NewWorkConfig(
			backup.Spec.Cluster,
			backup.Spec.Database,
			backup.Spec.Prefix,
			"/backup",
			backup.Spec.DestinationBucketSecretName,
		),
		portmaster.NewReconcilerConfig(
			r.Client,
			r.recorder,
			backup,
			r.statusMgr,
		),
		portmaster.WithWaitTimeout(r.Config.DefaultActionTimeout.Duration),
		portmaster.WithDevelopment(r.Config.Development),
		portmaster.WithBucketIgnoreTls(r.Config.BackupConfig.IgnoreTls),
		portmaster.WithImage(r.Config.BackupConfig.Image),
	)

	// spec changed, we need to reconcile everything and run a new backup job
	if !r.statusMgr.IsConditionTrueAndValid(portmaster.ConditionJobScheduled) {
		result, err := runner.ConnectDatabase(ctx)
		if err != nil {
			logger.Error(err, "failed to connect to database")
			return ctrl.Result{}, errors.Append(err)
		}

		if result.Requeue() {
			return ctrl.Result{}, nil
		}

		result, err = runner.ConnectBucket(ctx)
		if err != nil {
			logger.Error(err, "failed to connect to bucket")
			return ctrl.Result{}, errors.Append(err)
		}

		if result.Requeue() {
			return ctrl.Result{}, nil
		}

		result, err = runner.Reconcile(ctx)
		if err != nil {
			logger.Error(err, "failed to reconcile backup")
			return ctrl.Result{}, errors.Append(err)
		}

		if result.Requeue() {
			return ctrl.Result{}, nil
		}
	}

	completed, failed, err2 := runner.Status(ctx)
	if err2 != nil {
		_, err3 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, err2.Reason, err2.Err.Error())
		return ctrl.Result{}, errors.Append(err2, err3)
	}

	if completed {
		changed, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, ReasonBackupJobCompleted, "Backup job has completed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
		if changed {
			r.measureBackupReady(backup)
		}
	} else if failed {
		changed, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, ReasonBackupJobFailed, "Backup job has failed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
		if changed {
			r.measureBackupNotReady(backup)
		}
	}

	return ctrl.Result{}, errors.IfExists()
}

func (r *MongoDBBackupReconciler) measureBackupReady(backup *airlockv1alpha1.MongoDBBackup) {
	metrics.SetBackupReady(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

func (r *MongoDBBackupReconciler) measureBackupNotReady(backup *airlockv1alpha1.MongoDBBackup) {
	metrics.SetBackupNotReady(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

func (r *MongoDBBackupReconciler) handleDeletion(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) {
	metrics.RemoveBackupTotal(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

// SetupWithManager sets up the controller with the Manager
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("airlock")

	r.name = "MongoDBBackup"

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
