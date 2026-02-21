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
	"github.com/RocketChat/airlock/internal/metrics"
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
		// being deleted
		r.handleDeletion(ctx, backup)

		if controllerutil.RemoveFinalizer(backup, airlockFinalizer) {
			if err := r.Update(ctx, backup); err != nil {
				return ctrl.Result{}, errors.Append(err)
			}
		}

		return ctrl.Result{}, nil
	}

	r.statusMgr = conditions.NewManager(r.Client, backup, &backup.Status.Conditions)

	logger.Info("reconciling backup job")

	if r.statusMgr.IsConditionTrueAndValid(airlockv1alpha1.ConditionBackupJobScheduled) {
		// spec did not change, job has already finished
		// we don't care why this iteration got triggered.
		logger.Info("backup job has already finished, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, backup)

	var (
		accessRequest *airlockv1alpha1.MongoDBAccessRequest
		bucketSecret  *v1.Secret
		pvc           *v1.PersistentVolumeClaim
	)

	// spec changed, we need to reconcile everything and run a new backup job

	logger.Info("checking destination bucket condition", "name", backup.Spec.DestinationBucketSecretRef.Name, "namespace", backup.Spec.DestinationBucketSecretRef.Namespace)

	if !r.statusMgr.IsConditionTrueAndValid(airlockv1alpha1.ConditionBackupJobScheduled) {
		logger.Info("scheduled job may be stale, reconciling everything")

		logger.Info("validating destination bucket secret")
		bucketSecret, err = r.checkDestinationBucket(ctx, backup)
		if err != nil {
			changed, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonDestinationBucketInvalid, fmt.Sprintf("Failed to validate destination bucket: %s", err.Error()))

			if changed {
				r.measureBackupNotReady(backup)
			}

			return ctrl.Result{}, errors.Append(err, err2)
		}

		logger.Info("reconciling mongodb access request")
		accessRequest, err = r.reconcileAccessRequest(ctx, backup)
		if err != nil {
			logger.Error(err, "failed to reconcile mongodb access request")
			changed, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonAccessRequestNotReady, err.Error())
			if changed {
				r.measureBackupNotReady(backup)
			}
			return ctrl.Result{}, errors.Append(err, err2)
		}

		logger.Info("getting database size from mongodb for pvc sizing")
		size, err := getDatabaseSizeFromAccessRequest(ctx, r.Client, accessRequest)
		if err != nil {
			logger.Error(err, "failed to get database size from access request")
			changed, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, EventReasonAccessRequestNotReady, fmt.Sprintf("Failed to get database size from access request: %s", err.Error()))
			if changed {
				r.measureBackupNotReady(backup)
			}
			return ctrl.Result{}, errors.Append(err, err2)
		}

		pvc, err = reconciler.ReconcilePersistentVolumeClaim(ctx, backup.Name, backup.Namespace, size, reconcilerOpts)
		if err != nil {
			logger.Error(err, "failed to reconcile pvc")
			// TODO: better reason propagation
			changed, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "MongoDBPVCNotReady", fmt.Sprintf("Failed to reconcile pvc: %s", err.Error()))
			if changed {
				r.measureBackupNotReady(backup)
			}
			return ctrl.Result{}, errors.Append(err, err2)
		}
	}

	// job needs reconciling anyway as long as Ready!=True

	job, result, err := r.reconcileJob(ctx, backup, accessRequest.Spec.SecretName, pvc.Name, bucketSecret.Name)
	if err != nil {
		logger.Error(err, "failed to reconcile job")
		changed, err2 := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "JobScheduleFailed", fmt.Sprintf("Failed to reconcile job: %s", err.Error()))
		if changed {
			r.measureBackupNotReady(backup)
		}
		return ctrl.Result{}, errors.Append(err, err2)
	}

	if result == controllerutil.OperationResultCreated {
		logger.Info("new job created")
		backup.Status.Result = &airlockv1alpha1.MongoDBBackupStatusResult{
			JobRef: &airlockv1alpha1.MongoDBBackupStatusJobRef{
				ID:   string(job.GetUID()),
				Name: job.Name,
			},
		}
		backup.Status.StartTime = job.Status.StartTime
		r.measureBackupNotReady(backup)
		if _, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionBackupJobScheduled, metav1.ConditionTrue, "JobScheduled", "Backup job has been scheduled"); err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
		return ctrl.Result{}, nil
	}

	if hasJobCompleted(job) {
		r.recorder.Eventf(backup, corev1.EventTypeNormal, "JobCompleted", "Job has completed: %s", job.Name)
		backup.Status.CompletionTime = job.Status.CompletionTime
		changed, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionTrue, "JobCompleted", "Job has completed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
		if changed {
			r.measureBackupReady(backup)
		}
	} else if hasJobFailed(job) {
		r.recorder.Eventf(backup, corev1.EventTypeWarning, "JobFailed", "Job has failed: %s", job.Name)
		backup.Status.CompletionTime = job.Status.CompletionTime
		changed, err := r.statusMgr.SetCondition(ctx, airlockv1alpha1.ConditionReady, metav1.ConditionFalse, "JobFailed", "Job has failed")
		if err != nil {
			return ctrl.Result{}, errors.Append(err)
		}
		if changed {
			r.measureBackupNotReady(backup)
		}
		// no need to requeue
		return ctrl.Result{}, errors.IfExists()
	}

	return ctrl.Result{}, errors.IfExists()
}

func (r *MongoDBBackupReconciler) measureBackupReady(backup *airlockv1alpha1.MongoDBBackup) {
	metrics.SetBackupReady(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

func (r *MongoDBBackupReconciler) measureBackupNotReady(backup *airlockv1alpha1.MongoDBBackup) {
	metrics.SetBackupNotReady(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

func (r *MongoDBBackupReconciler) reconcileJob(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup, mongoSecretName, pvcName, bucketSecretName string) (*batchv1.Job, controllerutil.OperationResult, error) {
	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, backup)
	var job batchv1.Job
	job.Name = backup.Name
	job.Namespace = backup.Namespace
	environment := []v1.EnvVar{
		{
			Name: "DATABASE_URI",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: mongoSecretName,
					},
					Key: "connectionString",
				},
			},
		},
		{
			Name:  "DATABASE_NAME",
			Value: backup.Spec.Database,
		},
		{
			Name: "DESTINATION_BUCKET",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: bucketSecretName,
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
						Name: bucketSecretName,
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
						Name: bucketSecretName,
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
						Name: bucketSecretName,
					},
					Key: airlockv1alpha1.DestinationBucketSecretRefSecretAccessKey,
				},
			},
		},
	}
	mountPath := "/backups"
	args := []string{
		"backup",
		"--log-format=json",
		"--upload",
		"--target-database",
		"--split=true",
		"-r",
		backup.Spec.Prefix,
		mountPath, // working dir
	}
	if r.Config.Development {
		args = append(args, "--log-level=debug")
	} else {
		args = append(args, "--log-level=info")
	}
	if backup.Spec.Encryption.Enabled {
		args = append(args, "-e")
		// TODO: ignoring engine
		environment = append(environment, v1.EnvVar{
			Name: "AGE_PRIVATE_KEYS",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: backup.Spec.Encryption.AgeSecretRef.Name,
					},
					Key: "keys",
				},
			},
		})
	}
	result, err := reconciler.CreateOrPatch(ctx, &job, func() error {
		container := v1.Container{
			Image:           r.Config.BackupConfig.Image,
			ImagePullPolicy: v1.PullIfNotPresent,
			Args:            args,
			Name:            backup.Name,
			Env:             environment,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "backup-storage",
					MountPath: mountPath,
				},
			},
		}
		job.Spec.Template.Spec.Containers = []v1.Container{container}
		job.Spec.Template.Spec.Volumes = []v1.Volume{
			{
				Name: "backup-storage",
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			},
		}
		job.Spec.Template.Spec.RestartPolicy = v1.RestartPolicyNever
		return nil
	}, reconcilerOpts)

	if err != nil {
		return nil, controllerutil.OperationResultNone, err
	}

	return &job, result, nil
}

const (
	EventReasonDestinationBucketInvalid = "DestinationBucketInvalid"
)

func (r *MongoDBBackupReconciler) checkDestinationBucket(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) (*v1.Secret, error) {
	secret := &v1.Secret{}

	secret.Name = backup.Spec.DestinationBucketSecretRef.Name
	if backup.Spec.DestinationBucketSecretRef.Namespace != "" {
		secret.Namespace = backup.Spec.DestinationBucketSecretRef.Namespace
	} else {
		secret.Namespace = backup.Namespace
	}

	if err := r.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		err := fmt.Errorf("failed to get destination bucket secret: %w", err)
		r.recorder.Eventf(backup, corev1.EventTypeWarning, EventReasonDestinationBucketInvalid, "Destination bucket secret %s/%s not found: %s", secret.Namespace, secret.Name, err.Error())
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*1)
	defer cancel()

	if err := validateBucketExists(timeoutCtx, r.statusMgr, secret, r.Config); err != nil {
		err := fmt.Errorf("failed to validate destination bucket: %w", err)
		r.recorder.Eventf(backup, corev1.EventTypeWarning, EventReasonDestinationBucketInvalid, "Failed to validate destination bucket: %s", err.Error())
		return nil, err
	}

	return secret, nil
}

const (
	EventReasonAccessRequestNotReady = "MongoDBAccessRequestNotReady"
)

func (r *MongoDBBackupReconciler) reconcileAccessRequest(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) (*airlockv1alpha1.MongoDBAccessRequest, error) {
	reconcilerOpts := reconciler.NewOption(r.Client, r.recorder, backup)

	var waitUntilAccessRequestIsReadyFunc = func(ctx context.Context, object client.Object) error {
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*1)
		defer cancel()

		return waitUntilAccessRequestIsReady(timeoutCtx, r.Client, object.(*airlockv1alpha1.MongoDBAccessRequest))
	}

	accessRequest, err := reconciler.ReconcileAccessRequest(ctx, backup.Name, backup.Namespace, backup.Spec.Cluster, backup.Spec.Database, waitUntilAccessRequestIsReadyFunc, reconcilerOpts)
	if err != nil {
		err := fmt.Errorf("failed to reconcile access request: %w", err)
		r.recorder.Eventf(backup, corev1.EventTypeWarning, EventReasonAccessRequestNotReady, err.Error())
		return nil, err
	}

	return accessRequest, nil
}

func (r *MongoDBBackupReconciler) handleDeletion(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) {
	metrics.RemoveBackupTotal(backup.Namespace, backup.Name, backup.Spec.Cluster, backup.Spec.Database)
}

// SetupWithManager sets up the controller with the Manager
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("airlock")

	r.name = "MongoDBBackup"

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &airlockv1alpha1.MongoDBBackup{}, "spec.destinationBucketSecretRef.name", func(rawObj client.Object) []string {
		backup := rawObj.(*airlockv1alpha1.MongoDBBackup)
		return []string{backup.Spec.DestinationBucketSecretRef.Name}
	}); err != nil {
		return fmt.Errorf("failed to index spec.destinationBucketSecretRef.name field: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
