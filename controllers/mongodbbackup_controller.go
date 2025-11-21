package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackups/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *MongoDBBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the MongoDBBackup instance
	var backup airlockv1alpha1.MongoDBBackup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		log.Error(err, "unable to fetch MongoDBBackup")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if backup is already completed
	if backup.Status.Phase == "Completed" || backup.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	// Initialize status if empty
	if backup.Status.Phase == "" {
		backup.Status.Phase = "Pending"
		backup.Status.StartTime = &metav1.Time{Time: time.Now()}
		if err := r.Status().Update(ctx, &backup); err != nil {
			log.Error(err, "failed to update backup status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// Check if Job already exists
	jobName := fmt.Sprintf("%s-backup-job", backup.Name)
	var existingJob batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: backup.Namespace}, &existingJob)
	if err == nil {
		// Job exists, check its status
		return r.updateBackupStatusFromJob(ctx, &backup, &existingJob)
	} else if client.IgnoreNotFound(err) != nil {
		log.Error(err, "failed to get backup job")
		return ctrl.Result{}, err
	}

	// Create backup Job
	job, err := r.createBackupJob(ctx, &backup)
	if err != nil {
		log.Error(err, "failed to create backup job")
		r.updateBackupStatusFailed(ctx, &backup, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "failed to create Job")
		r.updateBackupStatusFailed(ctx, &backup, err.Error())
		return ctrl.Result{}, err
	}

	log.Info("created backup job", "job", jobName)

	backup.Status.Phase = "Running"
	if err := r.Status().Update(ctx, &backup); err != nil {
		log.Error(err, "failed to update backup status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}

func (r *MongoDBBackupReconciler) createBackupJob(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-backup-job", backup.Name)

	// Build connection string
	connectionString := fmt.Sprintf("mongodb://%s.%s.svc.cluster.local:27017",
		backup.Spec.MongoDBRef.Name, backup.Spec.MongoDBRef.Namespace)

	// Build environment variables for backup
	env := []corev1.EnvVar{
		{Name: "MONGODB_URI", Value: connectionString},
		{Name: "BACKUP_NAME", Value: backup.Name},
	}

	// Add S3 configuration if specified
	if backup.Spec.Storage.Type == "s3" && backup.Spec.Storage.S3 != nil {
		s3 := backup.Spec.Storage.S3
		env = append(env, []corev1.EnvVar{
			{Name: "S3_BUCKET", Value: s3.Bucket},
			{Name: "S3_PREFIX", Value: s3.Prefix},
			{Name: "AWS_REGION", Value: s3.Region},
			{Name: "AWS_S3_ENDPOINT", Value: s3.Endpoint},
			// AWS credentials from secret
			{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3.SecretRef.Name},
						Key:                  "accessKeyId",
					},
				},
			},
			{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3.SecretRef.Name},
						Key:                  "secretAccessKey",
					},
				},
			},
		}...)
	}

	// Add database/collection filters
	var databases, collections []string
	for _, ns := range backup.Spec.Namespaces {
		databases = append(databases, ns.Database)
		if len(ns.Collections) > 0 {
			collections = append(collections, ns.Collections...)
		}
	}

	// Set database and collection names as env vars
	if len(databases) > 0 {
		env = append(env, corev1.EnvVar{Name: "DB_NAME", Value: databases[0]}) // For now, support single DB
	}
	if len(collections) > 0 {
		env = append(env, corev1.EnvVar{Name: "COLLECTION_NAMES", Value: strings.Join(collections, ",")})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: backup.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "backup",
							Image: "airlock-backup:latest", // TODO: make this configurable
							Env:   env,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "backup-storage",
									MountPath: "/backups",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "backup-storage",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}

func (r *MongoDBBackupReconciler) updateBackupStatusFromJob(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup, job *batchv1.Job) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if job.Status.CompletionTime != nil {
		// Job completed successfully
		backup.Status.Phase = "Completed"
		backup.Status.CompletionTime = job.Status.CompletionTime
		backup.Status.BackupPath = "local:/backup/backup.archive"
		backup.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Message:            "Backup completed successfully",
			},
		}

		if err := r.Status().Update(ctx, backup); err != nil {
			log.Error(err, "failed to update backup status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if job.Status.Failed > 0 {
		// Job failed
		backup.Status.Phase = "Failed"
		backup.Status.CompletionTime = &metav1.Time{Time: time.Now()}
		backup.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Message:            "Backup job failed",
			},
		}

		if err := r.Status().Update(ctx, backup); err != nil {
			log.Error(err, "failed to update backup status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Job is still running
	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}

func (r *MongoDBBackupReconciler) updateBackupStatusFailed(ctx context.Context, backup *airlockv1alpha1.MongoDBBackup, message string) {
	backup.Status.Phase = "Failed"
	backup.Status.CompletionTime = &metav1.Time{Time: time.Now()}
	backup.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Message:            message,
		},
	}
	r.Status().Update(ctx, backup)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
