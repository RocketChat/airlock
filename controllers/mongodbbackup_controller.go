package controllers

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	/*
	 * get backup cr
	 * get job from this
	 * if not found we create for that
	 * create an accessrequest for creds
	 * use the cred for the backup job
	 * use the secret for s3 destination stuff
	 */

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

		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:               "Pending",
			Status:             metav1.ConditionUnknown,
			Reason:             "backup has not started yet",
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            "backup job has not been scheduled yet",
		})

		if err := r.Status().Update(ctx, &backup); err != nil {
			log.Error(err, "failed to update backup status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	jobName := fmt.Sprintf("%s-backup-job", backup.Name)
	var existingJob batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: backup.Namespace}, &existingJob)

	if client.IgnoreNotFound(err) != nil {
		// error that is NOT 404
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Status:             StatusBackupFailed,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Type:               "Ready",
			Reason:             "backup job not found",
			Message:            "",
		})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, &backup)})
	}

	// np job so we create one
	// accessrequest for creds
	// collection names as required
	// use cluster for name of image to use
	var accessRequest airlockv1alpha1.MongoDBAccessRequest
	err = r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-access", backup.Name), Namespace: backup.Namespace}, &accessRequest)
	if err != nil {
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Type:               "Ready",
			Reason:             "AccessRequestNotFound",
			Message:            fmt.Sprintf("Failed to get MongoDBAccessRequest %s-access: %s", backup.Name, err.Error()),
		})

		backup.Status.Phase = StatusBackupFailed

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, &backup)})
	}

	// TODO: Continue with job creation logic
	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *MongoDBBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
