package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

// MongoDBBackupReconciler reconciles a MongoDBBackup object
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
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch,resourceNames=*

func (r *MongoDBBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var backup airlockv1alpha1.MongoDBBackup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		log.Error(err, "unable to fetch MongoDBBackup")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// nothing to do if we already have updated the status of the "backup"
	// TODO: likely since a job, we should retry checking the job status and update the state here
	if backup.Status.Phase == StatusBackupCompleted || backup.Status.Phase == StatusBackupFailed {
		return ctrl.Result{}, nil
	}

	if backup.Status.Phase == "" {
		backup.Status.Phase = StatusBackupPending

		backup.Status.StartTime = &metav1.Time{Time: time.Now()}

		// meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
		// 	Type:               "Pending",
		// 	Status:             metav1.ConditionUnknown,
		// 	Reason:             "backup has not started yet",
		// 	LastTransitionTime: metav1.NewTime(time.Now()),
		// 	Message:            "backup job has not been scheduled yet",
		// })

		if err := r.Status().Update(ctx, &backup); err != nil {
			log.Error(err, "failed to update backup status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// now use the secret as a reference for all mongo env vars
	// use the backup.Spec.backupBucketSecret for the same purpose
	var backupJob = batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backup.Name,
			Namespace: backup.Namespace,
		},
	}

	err := r.Get(ctx, client.ObjectKeyFromObject(&backupJob), &backupJob)

	if client.IgnoreNotFound(err) != nil {
		// FIXME: handle it
		return ctrl.Result{}, err
	}

	var maxParallel int32 = 1

	backupImage, err := r.getBackupImage(ctx, backup.Spec.Cluster)
	if err != nil {
		//FIXME: handle non nil including when the referenced cluster does not exist
		return ctrl.Result{}, err
	}

	envVars, err := r.getMongoDbEnvVars(ctx, backup)
	if err != nil {
	}

	backupJob.Spec = batchv1.JobSpec{
		Parallelism: &maxParallel,
		// Completions: 1,
		Template: v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				RestartPolicy: v1.RestartPolicyNever,
				Containers: []v1.Container{
					{
						ImagePullPolicy: v1.PullIfNotPresent,
						Name:            backup.Name,
						Image:           backupImage,
						Command:         []string{"sleep", "1d"},
						Env:             *envVars,
					},
				},
			},
		},
	}

	controllerutil.SetControllerReference(&backup, &backupJob, r.Scheme)

	err = r.Create(ctx, &backupJob)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *MongoDBBackupReconciler) getBackupImage(ctx context.Context, cluster string) (string, error) {
	var mongodbCluster = airlockv1alpha1.MongoDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster,
		},
	}

	err := r.Get(ctx, client.ObjectKeyFromObject(&mongodbCluster), &mongodbCluster)
	if err != nil {
		return "", err
	}

	return mongodbCluster.Spec.BackupImage, nil
}

func (r *MongoDBBackupReconciler) getMongoDbEnvVars(ctx context.Context, backup airlockv1alpha1.MongoDBBackup) (*[]v1.EnvVar, error) {
	var accessRequest airlockv1alpha1.MongoDBAccessRequest

	accessRequest.Name = fmt.Sprintf("%s-access", backup.Name)
	accessRequest.Namespace = backup.Namespace

	err := r.Get(ctx, client.ObjectKeyFromObject(&accessRequest), &accessRequest)
	if client.IgnoreNotFound(err) != nil {
		// FIXME: handle error here
	}

	err = nil

	accessRequest.Spec.ClusterName = backup.Spec.Cluster
	accessRequest.Spec.Database = backup.Spec.Database
	accessRequest.Spec.UserName = backup.Name + "-user"

	controllerutil.SetControllerReference(&backup, &accessRequest, r.Scheme)

	err = r.Create(ctx, &accessRequest)
	if err != nil {
		// TODO: handle this error
	}

	secretRef := accessRequest.Name

	return &[]v1.EnvVar{
		getEnvVarFromSecret("MONGODB_URI", secretRef, "connectionString"),
		getEnvVar("DATABASE", backup.Spec.Database),
		getEnvVar("COLLECTIONS", strings.Join(backup.Spec.IncludedCollections, ",")),
		getEnvVar("EXCLUDED_COLLECTIONS", strings.Join(backup.Spec.ExcludedCollections, ",")),
	}, nil
}

func getEnvVar(name, value string) v1.EnvVar {
	return v1.EnvVar{
		Name:  name,
		Value: value,
	}
}

func getEnvVarFromSecret(name, secretRef, key string) v1.EnvVar {
	return v1.EnvVar{
		Name: name,
		ValueFrom: &v1.EnvVarSource{
			SecretKeyRef: &v1.SecretKeySelector{
				Key: key,
				LocalObjectReference: v1.LocalObjectReference{
					Name: secretRef,
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&airlockv1alpha1.MongoDBAccessRequest{}).
		Complete(r)
}
