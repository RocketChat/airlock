package portmaster

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/pkg/reconciler"
	pmmongo "github.com/RocketChat/portmaster/pkg/database/mongo"
	"github.com/RocketChat/portmaster/pkg/manifest"
)

type Result interface {
	Requeue() bool
}

type requeueResult struct{}
type noRequeueResult struct{}

func (r *requeueResult) Requeue() bool {
	return true
}

func (r *noRequeueResult) Requeue() bool {
	return false
}

func Requeue() Result {
	return &requeueResult{}
}

func NoRequeue() Result {
	return &noRequeueResult{}
}

// PortmasterRecnciler emulates the cli through steps that needs be performed for the directed mode.
// hides the details of k8s api.
type PortmasterRecnciler interface {
	// ConnectDatabase ensures a connection to the database is made.
	ConnectDatabase(ctx context.Context) (Result, *ReconcilerError)

	// ConnectBucket ensures a the bucket exists and is accessible.
	ConnectBucket(ctx context.Context) (Result, *ReconcilerError)

	// Cleanup cleans up the resources created by the runner that may no longer be needed.
	Cleanup(ctx context.Context) *ReconcilerError

	// Run starts portmaster in the directed mode.
	Reconcile(ctx context.Context) (Result, *ReconcilerError)

	Status(ctx context.Context) (completed, failed bool, err *ReconcilerError)
}

type PortmasterMode string

const (
	PortmasterModeExportDatabase PortmasterMode = "export-database"
	PortmasterModeImportDatabase PortmasterMode = "import-database"
	PortmasterModeExportFiles    PortmasterMode = "export-files"
	PortmasterModeImportFiles    PortmasterMode = "import-files"

	// condition this reconciler handles
	ConditionJobScheduled = "JobScheduled"
)

type portmasterRunner struct {
	options *Options

	// constructor
	namespace string
	name      string

	// ConnectDatabase
	recreate                bool
	databaseUri             string
	accessRequestSecretName string

	// ConnectBucket
	s3Client *s3.Client
	bucket   string

	// constructor
	logger *logr.Logger
}

// Completed implements [PortmasterRecnciler].
func (p *portmasterRunner) Status(ctx context.Context) (completed, failed bool, err *ReconcilerError) {
	job, err2 := p.getJob(ctx, p.name)
	if err2 != nil {
		if apierrors.IsNotFound(err2) {
			_, err3 := p.options.statusMgr.SetCondition(ctx, ConditionJobScheduled, metav1.ConditionFalse, "JobNotFound", "Job not found")

			return false, false, NewRuntimeError(errors.Join(err2, err3))
		}
		return false, false, NewRuntimeError(err2)
	}

	return p.hasJobCompleted(job), p.hasJobFailed(job), nil
}

func NewPortmasterRunner(ctx context.Context, fn ...OptionProvider) PortmasterRecnciler {
	options := &Options{}
	for _, fn := range fn {
		fn(options)
	}

	logger := log.FromContext(ctx).WithValues("component", "portmaster")

	return &portmasterRunner{
		options:   options,
		logger:    &logger,
		namespace: options.owner.GetNamespace(),
		name:      fmt.Sprintf("%s-%s", options.owner.GetName(), options.mode),
	}
}

// ConnectDatabase implements [PortmasterRecnciler].
func (p *portmasterRunner) ConnectDatabase(ctx context.Context) (Result, *ReconcilerError) {
	p.logger.Info("reconciling access request")
	accessRequest, result, err := reconciler.ReconcileAccessRequest(
		ctx,
		p.name,
		p.options.owner.GetNamespace(),
		p.options.cluster,
		p.options.database,
		"", // use default from controller
		p.waitUntilAccessRequestIsReady,
		reconciler.NewOption(p.options.k8sClient, p.options.eventRecorder, p.options.owner),
	)
	if err != nil {
		p.logger.Error(err, "failed to reconcile access request")
		reconcilerErr, yes := err.(*ReconcilerError)
		if yes {
			return nil, reconcilerErr
		}

		return nil, NewRuntimeError(err)
	}

	if !meta.IsStatusConditionTrue(accessRequest.Status.Conditions, airlockv1alpha1.ConditionReady) {
		return Requeue(), NewMongoDBAccessRequestReadyTimeoutError(fmt.Errorf("access request is not ready"))
	}

	p.recreate = result == controllerutil.OperationResultUpdated
	p.accessRequestSecretName = accessRequest.Spec.SecretName

	p.logger.Info("access request is ready")

	secret, err := p.getSecret(
		ctx,
		p.accessRequestSecretName,
	)
	if err != nil {
		return nil, NewRuntimeError(fmt.Errorf("failed to get access request secret: %w", err))
	}

	data := secret.Data

	connectionString, ok := getStringMapValue(data, "connectionString")
	if !ok {
		return nil, NewSecretKeyNotFoundError(fmt.Errorf("connectionString key not found in mongodb access request secret %s", p.accessRequestSecretName))
	}

	p.databaseUri = connectionString

	mongoClient, err := pmmongo.New(connectionString, "")
	if err != nil {
		return nil, NewMongoDBConnectionError(fmt.Errorf("failed to create mongodb client: %w", err))
	}

	if err := mongoClient.Ping(); err != nil {
		return nil, NewMongoDBConnectionError(fmt.Errorf("failed to ping mongodb: %w", err))
	}

	p.logger.Info("database is ready")

	return NoRequeue(), nil
}

func (p *portmasterRunner) waitUntilAccessRequestIsReady(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, p.options.waitTimeout)
	defer cancel()
	if err := wait.PollUntilContextCancel(timeoutCtx, time.Second*5, true, func(ctx context.Context) (done bool, err error) {
		p.logger.Info("waiting for access request to be ready")

		obj := &airlockv1alpha1.MongoDBAccessRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      p.name,
				Namespace: p.namespace,
			},
		}
		if err := p.options.k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(obj.Status.Conditions, airlockv1alpha1.ConditionReady) {
			return true, nil
		}

		return false, nil
	}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return NewMongoDBAccessRequestReadyTimeoutError(err)
		}

		return err
	}

	return nil
}

// Cleanup implements [PortmasterRecnciler].
func (p *portmasterRunner) Cleanup(ctx context.Context) *ReconcilerError {
	panic("unimplemented")
}

// ConnectBucket implements [PortmasterRecnciler].
func (p *portmasterRunner) ConnectBucket(ctx context.Context) (Result, *ReconcilerError) {
	secret, err := p.getSecret(ctx, p.options.bucketSecretName)
	if err != nil {
		return nil, NewRuntimeError(err)
	}

	data := secret.Data

	bucket, ok := getStringMapValue(data, bucketSecretKeys.bucket)
	if !ok {
		return nil, NewSecretKeyNotFoundError(fmt.Errorf("bucket key %s not found in secret %s", bucketSecretKeys.bucket, p.options.bucketSecretName))
	}

	region, ok := getStringMapValue(data, bucketSecretKeys.region)
	if !ok {
		return nil, NewSecretKeyNotFoundError(fmt.Errorf("region key %s not found in secret %s", bucketSecretKeys.region, p.options.bucketSecretName))
	}

	accessKeyId, ok := getStringMapValue(data, bucketSecretKeys.accessKeyId)
	if !ok {
		return nil, NewSecretKeyNotFoundError(fmt.Errorf("accessKeyId key %s not found in secret %s", bucketSecretKeys.accessKeyId, p.options.bucketSecretName))
	}

	secretAccessKey, ok := getStringMapValue(data, bucketSecretKeys.secretAccessKey)
	if !ok {
		return nil, NewSecretKeyNotFoundError(fmt.Errorf("secretAccessKey key %s not found in secret %s", bucketSecretKeys.secretAccessKey, p.options.bucketSecretName))
	}

	p.s3Client = newS3Client(ctx, region, accessKeyId, secretAccessKey, p.options.bucketIgnoreTls)

	bucketCtx, cancel := context.WithTimeout(ctx, p.options.waitTimeout)
	defer cancel()

	if err := validateBucketExists(
		bucketCtx,
		p.s3Client,
		bucket,
	); err != nil {
		return nil, NewInvalidDestinationBucketError(err)
	}

	p.bucket = bucket

	return NoRequeue(), nil
}

// Run implements [PortmasterRecnciler].
func (p *portmasterRunner) Reconcile(ctx context.Context) (Result, *ReconcilerError) {
	existingJob, err := p.getJob(ctx, p.name)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, NewRuntimeError(err)
		}
	} else {
		if !existingJob.DeletionTimestamp.IsZero() {
			p.logger.Info("job is being deleted, requeuing")
			return Requeue(), nil
		}

		// this is the happy path
		// it's also possible that we may have lost this particular state, in which case, a DeepDerivate will detect the skew
		if p.recreate {
			p.logger.Info("access request changed, deleting existing job")
			if err := reconciler.Delete(
				ctx,
				existingJob,
				reconciler.NewOption(p.options.k8sClient, p.options.eventRecorder, p.options.owner),
			); err != nil {
				return nil, NewRuntimeError(err)
			}

			_, err2 := p.options.statusMgr.SetCondition(ctx, ConditionJobScheduled, metav1.ConditionFalse, "JobDeleted", "Stale job deleted")
			if err2 != nil {
				return nil, NewRuntimeError(fmt.Errorf("failed to set job deleted condition: %w", err2))
			}

			p.logger.Info("job deleted, requeuing")

			// allow to be requeued to wait for the deletion to complete
			return Requeue(), nil
		}
	}

	reconcilePvc := false

	var pvcRequest *resource.Quantity = nil

	pvc, err := p.getPVC(ctx, p.name)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, NewRuntimeError(fmt.Errorf("failed to get pvc: %w", err))
		}

		p.logger.Info("pvc does not exist, creating")
		reconcilePvc = true
	} else {
		p.logger.Info("pvc exists, checking if it needs to be resized")
		rawSize, err := p.getRequiredDiskSizeEstimate(ctx)
		if err != nil {
			return nil, NewDiskSizeEstimateError(fmt.Errorf("failed to get required disk size estimate: %w", err))
		}

		if rawSize == 0 {
			return nil, NewDiskSizeEstimateError(fmt.Errorf("required disk size estimate is 0, no operation to perform"))
		}

		currentSize := pvc.Spec.Resources.Requests.Storage().AsApproximateFloat64()

		if p.shouldResizePVC(rawSize, currentSize) {
			reconcilePvc = true
			finalSize := p.addDiskUsageOverhead(rawSize)
			pvcRequest = resource.NewQuantity(int64(finalSize), resource.BinarySI)
			p.logger.Info("pvc needs to be resized", "request", pvcRequest.String(), "oldSize", pvc.Spec.Resources.Requests.Storage().String())
		} else {
			p.logger.Info("pvc does not need to be resized, skipping")
		}
	}

	if reconcilePvc {
		if pvcRequest == nil {
			p.logger.Info("estimating required disk size for new pvc")
			size, err := p.getRequiredDiskSizeEstimate(ctx)
			if err != nil {
				return nil, NewDiskSizeEstimateError(fmt.Errorf("failed to get required disk size estimate: %w", err))
			}

			if size == 0 {
				return nil, NewDiskSizeEstimateError(fmt.Errorf("required disk size estimate is 0, no operation to perform"))
			}

			finalSize := p.addDiskUsageOverhead(size)
			pvcRequest = resource.NewQuantity(int64(finalSize), resource.BinarySI)
		}

		_, _, err = reconciler.ReconcilePersistentVolumeClaim(
			ctx,
			p.name,
			p.namespace,
			pvcRequest,
			reconciler.NewOption(p.options.k8sClient, p.options.eventRecorder, p.options.owner),
		)
		if err != nil {
			return nil, NewRuntimeError(fmt.Errorf("failed to reconcile persistent volume claim: %w", err))
		}

		p.logger.Info("pvc reconciled")
	}

	podSpec := p.getPodSpec()

	if existingJob != nil &&
		!apiequality.Semantic.DeepDerivative(podSpec, existingJob.Spec.Template.Spec) {
		p.logger.Info("pod template spec has changed, deleting existing job")
		if err := reconciler.Delete(
			ctx,
			existingJob,
			reconciler.NewOption(p.options.k8sClient, p.options.eventRecorder, p.options.owner),
		); err != nil {
			return nil, NewRuntimeError(err)
		}

		_, err2 := p.options.statusMgr.SetCondition(ctx, ConditionJobScheduled, metav1.ConditionFalse, "JobDeleted", "Stale job deleted")
		if err2 != nil {
			return nil, NewRuntimeError(fmt.Errorf("failed to set job deleted condition: %w", err2))
		}

		p.logger.Info("job deleted, requeuing")

		return Requeue(), nil
	} else if existingJob == nil {
		p.logger.Info("creating new job")
		_, err = p.createJob(ctx, podSpec)
		if err != nil {
			return nil, NewRuntimeError(err)
		}

		_, err2 := p.options.statusMgr.SetCondition(ctx, ConditionJobScheduled, metav1.ConditionTrue, "JobCreated", "Job created")
		if err2 != nil {
			return nil, NewRuntimeError(fmt.Errorf("failed to set job created condition: %w", err2))
		}

		p.logger.Info("job created, requeuing")

		return Requeue(), nil
	}

	return NoRequeue(), nil
}

func (p *portmasterRunner) cliArgs() []string {
	args := []string{
		"--log-format=json",
	}

	if p.options.exporting {
		args = append(args, "export", "--upload")
	}

	if p.options.importing {
		args = append(args, "import")
	}

	if p.options.development {
		args = append(args, "--log-level=debug")
	} else {
		args = append(args, "--log-level=info")
	}

	if p.options.targetFiles {
		args = append(args, "--target-files")
	}

	if p.options.targetDatabase {
		args = append(args, "--target-database")
	}

	args = append(args, "--split=true", "-r", p.options.remotePrefix, p.options.workingDirectory)

	return args
}

func (p *portmasterRunner) getPodSpec() *corev1.PodSpec {
	env := []v1.EnvVar{
		{
			Name: "DATABASE_URI",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.accessRequestSecretName,
					},
					Key: "connectionString",
				},
			},
		},
		{
			Name:  "DATABASE_NAME",
			Value: p.options.database,
		},
		{
			Name: "DESTINATION_BUCKET",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.options.bucketSecretName,
					},
					Key: bucketSecretKeys.bucket,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_REGION",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.options.bucketSecretName,
					},
					Key: bucketSecretKeys.region,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_ACCESS_KEY_ID",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.options.bucketSecretName,
					},
					Key: bucketSecretKeys.accessKeyId,
				},
			},
		},
		{
			Name: "DESTINATION_BUCKET_SECRET_ACCESS_KEY",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: p.options.bucketSecretName,
					},
					Key: bucketSecretKeys.secretAccessKey,
				},
			},
		},
	}
	mountPath := fmt.Sprintf("%c%s", os.PathSeparator, p.options.mode)
	mountName := fmt.Sprintf("%s-storage", p.options.mode)
	args := p.cliArgs()
	container := v1.Container{
		Image:           p.options.image,
		ImagePullPolicy: v1.PullIfNotPresent,
		Args:            args,
		Name:            p.name,
		Env:             env,
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      mountName,
				MountPath: mountPath,
			},
		},
	}
	podSpec := &corev1.PodSpec{
		Containers: []v1.Container{container},
		Volumes: []v1.Volume{
			{
				Name: mountName,
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: p.name,
					},
				},
			},
		},
	}
	return podSpec
}

func (p *portmasterRunner) createJob(ctx context.Context, podTemplateSpec *corev1.PodSpec) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.name,
			Namespace: p.namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: *podTemplateSpec,
			},
		},
	}

	err := reconciler.Create(ctx, job, reconciler.NewOption(p.options.k8sClient, p.options.eventRecorder, p.options.owner))
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (p *portmasterRunner) addDiskUsageOverhead(size uint64) uint64 {
	// a constant overhead of 20%
	// database, this is more than enough
	// especially if we exclude collections, but calculate full db size.
	// it's better to be safe than sorry.
	// files, post archiving, even the *2 estimate is enough,
	// the excess 20% is to just be on the safe side for new files added and avoiding resizing
	return uint64(math.Ceil(float64(size) * 1.2))
	// note that this constant overhead is BAD for huge sources,
	// TODO: add a scaling procedure to determine the overhead based on the size of the source
}

func (p *portmasterRunner) shouldResizePVC(rawSize uint64, currentSize float64) bool {
	// return false
	return rawSize > uint64(currentSize*0.95)
}

func (p *portmasterRunner) getRequiredDiskSizeEstimate(ctx context.Context) (uint64, error) {
	if p.options.exporting {
		m, err := pmmongo.New(p.databaseUri, "")
		if err != nil {
			return 0, err
		}

		if p.options.targetDatabase {
			size, err := m.GetDatabaseMaxSizeEstimate(ctx)
			if err != nil {
				return 0, err
			}

			return size, nil
		}

		if p.options.targetFiles {
			size, err := m.GetTotalFilesDiskUsage(ctx, 4096)
			if err != nil {
				return 0, err
			}

			return size * 2, nil
		}
	}

	if p.options.importing {
		var manifestName string
		// for files we will doubble the required disk space before adding overhead
		var multiplier uint64 = 1
		if p.options.targetFiles {
			manifestName = manifest.FilesManifestName
			multiplier = 2
		}

		if p.options.targetDatabase {
			manifestName = manifest.DatabaseManifestName
		}

		path := filepath.Join(p.options.remotePrefix, manifestName)

		resp, err := p.s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(p.bucket),
			Key:    aws.String(path),
		})
		if err != nil {
			return 0, err
		}

		defer resp.Body.Close()

		m, err := manifest.Load(resp.Body)
		if err != nil {
			return 0, err
		}

		return m.EstimatedDiskUsage() * multiplier, nil
	}

	return 0, fmt.Errorf("portmaster is not configured to export or import")
}

func (p *portmasterRunner) getSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.namespace,
		},
	}

	if err := p.options.k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		return nil, err
	}

	return secret, nil
}

func (p *portmasterRunner) getJob(ctx context.Context, name string) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.namespace,
		},
	}

	if err := p.options.k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
		return nil, err
	}

	return job, nil
}

func (p *portmasterRunner) getPVC(ctx context.Context, name string) (*v1.PersistentVolumeClaim, error) {
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.namespace,
		},
	}

	if err := p.options.k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), pvc); err != nil {
		return nil, err
	}

	return pvc, nil
}

func (p *portmasterRunner) hasJobCompleted(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

func (p *portmasterRunner) hasJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
