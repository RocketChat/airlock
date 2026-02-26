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
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/config"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/pkg/conditions"
	"github.com/RocketChat/airlock/pkg/portmaster"
)

type MongoDBRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *config.Config

	name      string
	statusMgr *conditions.ConditionsManager
	recorder  record.EventRecorder
}

const (
	EventReasonVolumeSizeUnknown = "VolumeSizeUnknown"
)

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbrestores/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MongoDBRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	errors := internalerrors.New()
	defer measureControllerReconciliation(r.name, time.Now(), errors)

	logger := log.FromContext(ctx)

	restore := &airlockv1alpha1.MongoDBRestore{}
	err := r.Get(ctx, req.NamespacedName, restore)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.statusMgr = conditions.NewManager(r.Client, restore, &restore.Status.Conditions)

	if r.statusMgr.IsConditionTrueAndValid(airlockv1alpha1.ConditionReady) {
		logger.Info("restore is already complete, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	runner := portmaster.NewPortmasterRunner(
		ctx,
		portmaster.WithOwner(restore),
		portmaster.WithK8sClient(r.Client),
		portmaster.WithEventRecorder(r.recorder),
		portmaster.WithCluster(restore.Spec.Cluster),
		portmaster.WithDatabase(restore.Spec.Database),
		portmaster.WithBucketSecretName(restore.Spec.SourceBucketSecretName),
		portmaster.WithRemotePrefix(restore.Spec.Prefix),
		portmaster.WithMode(portmaster.PortmasterModeImportDatabase),
		portmaster.WithBucketIgnoreTls(r.Config.BackupConfig.IgnoreTls),
		portmaster.WithImage(r.Config.BackupConfig.Image),
		portmaster.WithWaitTimeout(time.Minute*5),
		portmaster.WithDevelopment(r.Config.Development),
		portmaster.WithWorkingDirectory("/restore"),
		portmaster.WithStatusMgr(r.statusMgr),
	)

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
			logger.Error(err, "failed to reconcile restore")
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
		_, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, "JobCompleted", "Restore job has completed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else if failed {
		_, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "JobFailed", "Restore job has failed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	}

	return ctrl.Result{}, errors.IfExists()
}

func (r *MongoDBRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("airlock")

	r.name = "MongoDBRestore"

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBRestore{}).
		Owns(&batchv1.Job{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Complete(r)
}
