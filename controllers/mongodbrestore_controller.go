package controllers

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/controllers/reconciler"
	"github.com/RocketChat/airlock/internal/conditions"
	"github.com/RocketChat/airlock/internal/config"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/portmaster-v2/pkg/manifest"
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

	logger.Info("reconciling restore", "name", req.NamespacedName)

	r.statusMgr = conditions.NewManager(r.Client, restore, &restore.Status.Conditions)

	var (
		accessRequest *airlockv1alpha1.MongoDBAccessRequest
		bucketSecret  *v1.Secret
		pvc           *v1.PersistentVolumeClaim
	)

	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, restore)

	if !r.statusMgr.IsConditionTrueAndValid(airlockv1alpha1.ConditionBackupJobScheduled) {
		logger.Info("scheduled job may be stale, reconciling everything")

		logger.Info("checking destination bucket")
		bucketSecret, err = r.checkDestinationBucket(ctx, restore)
		if err != nil {
			err := fmt.Errorf("failed to validate destination bucket: %w", err)
			r.recorder.Event(restore, corev1.EventTypeWarning, EventReasonDestinationBucketInvalid, err.Error())
			_, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonDestinationBucketInvalid, err.Error())
			return ctrl.Result{}, errors.Append(err, err2)
		}

		bucket, region, accessKeyId, secretAccessKey, _ := getS3PropertiesFromSecret(bucketSecret)

		logger.Info("getting volume size from destination bucket manifest")
		size, err := getVolumeSizeFromDestinationBucketManifest(ctx, bucket, restore.Spec.Prefix, region, accessKeyId, secretAccessKey, manifest.DatabaseManifestName)
		if err != nil {
			err := fmt.Errorf("failed to get volume size from destination bucket manifest: %w", err)
			errors.Append(err)
			r.recorder.Event(restore, corev1.EventTypeWarning, EventReasonVolumeSizeUnknown, err.Error())
			reason := "DestinationBucketManifestFetchFailed"
			_, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, reason, err.Error())
			return ctrl.Result{}, errors.Append(err, err2)
		}

		logger.Info("reconciling mongodb access request")
		accessRequest, err = r.reconcileAccessRequest(ctx, restore)
		if err != nil {
			_, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonAccessRequestNotReady, err.Error())
			return ctrl.Result{}, errors.Append(err, err2)
		}

		pvc, err = reconciler.ReconcilePersistentVolumeClaim(ctx, restore.Name, restore.Namespace, int64(size), reconcilerOpts)
		if err != nil {
			logger.Error(err, "failed to reconcile pvc")
			err := fmt.Errorf("failed to reconcile pvc: %w", err)
			errors.Append(err)
			// TODO: better event propagation
			reason := "PersistentVolumeClaimNotReady"
			_, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, reason, err.Error())
			return ctrl.Result{}, errors.Append(err, err2)
		}
	}

	job, result, err := r.reconcileJob(ctx, restore, accessRequest, pvc, bucketSecret)
	if err != nil {
		logger.Error(err, "failed to reconcile job")

		_, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "JobScheduleFailed", fmt.Sprintf("Failed to reconcile job: %s", err.Error()))

		return ctrl.Result{}, errors.Append(err, err2)
	}

	if result == controllerutil.OperationResultCreated {
		restore.Status.Result = &airlockv1alpha1.MongoDBRestoreStatusResult{
			JobRef: &airlockv1alpha1.MongoDBRestoreStatusJobRef{
				ID:   string(job.GetUID()),
				Name: job.Name,
			},
		}

		restore.Status.StartTime = job.Status.StartTime

		_, err = r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, "JobScheduled", "Job has been scheduled")

		return ctrl.Result{}, errors.Append(err)
	}

	if hasJobCompleted(job) {
		restore.Status.CompletionTime = job.Status.CompletionTime

		_, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, "JobCompleted", "Job has completed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	} else if hasJobFailed(job) {
		restore.Status.CompletionTime = job.Status.CompletionTime

		_, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "JobFailed", "Job has failed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
	}

	return ctrl.Result{RequeueAfter: time.Second * 5}, errors.IfExists()
}

func (r *MongoDBRestoreReconciler) reconcileAccessRequest(ctx context.Context, restore *airlockv1alpha1.MongoDBRestore) (*airlockv1alpha1.MongoDBAccessRequest, error) {
	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, restore)

	var waitUntilAccessRequestIsReadyFunc = func(ctx context.Context, object client.Object) error {
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*1)
		defer cancel()

		return waitUntilAccessRequestIsReady(timeoutCtx, r.Client, object.(*airlockv1alpha1.MongoDBAccessRequest))
	}

	accessRequest, err := reconciler.ReconcileAccessRequest(ctx, restore.Name, restore.Namespace, restore.Spec.Cluster, restore.Spec.Database, waitUntilAccessRequestIsReadyFunc, reconcilerOpts)
	if err != nil {
		err := fmt.Errorf("failed to reconcile access request: %w", err)
		r.recorder.Eventf(restore, corev1.EventTypeWarning, EventReasonAccessRequestNotReady, err.Error())
		return nil, err
	}

	return accessRequest, nil
}

func (r *MongoDBRestoreReconciler) checkDestinationBucket(ctx context.Context, restore *airlockv1alpha1.MongoDBRestore) (*v1.Secret, error) {
	secret := &v1.Secret{}

	secret.Name = restore.Spec.DestinationBucketSecretRef.Name
	if restore.Spec.DestinationBucketSecretRef.Namespace != "" {
		secret.Namespace = restore.Spec.DestinationBucketSecretRef.Namespace
	} else {
		secret.Namespace = restore.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		err := fmt.Errorf("failed to get destination bucket secret: %w", err)
		r.recorder.Eventf(restore, corev1.EventTypeWarning, EventReasonDestinationBucketInvalid, "Destination bucket secret %s/%s not found: %s", secret.Namespace, secret.Name, err.Error())
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*1)
	defer cancel()

	if err := validateBucketExists(timeoutCtx, r.statusMgr, secret, r.Config); err != nil {
		err := fmt.Errorf("failed to validate destination bucket: %w", err)
		r.recorder.Eventf(restore, corev1.EventTypeWarning, EventReasonDestinationBucketInvalid, "Failed to validate destination bucket: %s", err.Error())
		return nil, err
	}

	return secret, nil
}

func (r *MongoDBRestoreReconciler) reconcileJob(ctx context.Context, restore *airlockv1alpha1.MongoDBRestore, accessRequest *airlockv1alpha1.MongoDBAccessRequest, pvc *v1.PersistentVolumeClaim, bucketSecret *v1.Secret) (*batchv1.Job, controllerutil.OperationResult, error) {
	logger := log.FromContext(ctx)

	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, restore)

	var job batchv1.Job
	job.Name = restore.Name
	job.Namespace = restore.Namespace

	environment := []v1.EnvVar{
		{
			Name: "DATABASE_URI",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: accessRequest.Spec.SecretName,
					},
					Key: "connectionString",
				},
			},
		},
		{
			Name:  "DATABASE_NAME",
			Value: restore.Spec.Database,
		},
		{
			Name: "DESTINATION_BUCKET",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: bucketSecret.Name,
					},
					Key: airlockv1alpha1.DestinationBucketSecretRefBucket,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_REGION",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: bucketSecret.Name,
					},
					Key: airlockv1alpha1.DestinationBucketSecretRefRegion,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_ACCESS_KEY_ID",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: bucketSecret.Name,
					},
					Key: airlockv1alpha1.DestinationBucketSecretRefAccessKeyID,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_SECRET_ACCESS_KEY",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: bucketSecret.Name,
					},
					Key: airlockv1alpha1.DestinationBucketSecretRefSecretAccessKey,
				},
			},
		},
	}
	mountPath := "/imports"
	args := []string{
		"import",
		"--log-format=json",
		"--log-level=info",
		"--target-database",
		"-r",
		restore.Spec.Prefix,
		mountPath, // working dir
	}
	if restore.Spec.DropDatabase {
		args = append(args, "-D")
	}
	result, err := reconciler.CreateOrPatch(ctx, &job, func() error {
		container := v1.Container{
			Image:           r.Config.BackupConfig.Image,
			ImagePullPolicy: v1.PullIfNotPresent,
			Args:            args,
			Name:            restore.Name,
			Env:             environment,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "import-storage",
					MountPath: mountPath,
				},
			},
		}
		job.Spec.Template.Spec.Containers = []v1.Container{container}
		job.Spec.Template.Spec.Volumes = []v1.Volume{
			{
				Name: "import-storage",
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc.Name,
					},
				},
			},
		}
		job.Spec.Template.Spec.RestartPolicy = v1.RestartPolicyNever
		return nil
	}, reconcilerOpts)
	if err != nil {
		logger.Error(err, "failed to reconcile job")
		return nil, controllerutil.OperationResultNone, err
	}

	return &job, result, nil
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
