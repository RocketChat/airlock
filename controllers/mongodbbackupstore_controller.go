package controllers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/conditions"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/go-logr/logr"
)

// MongoDBBackupReconciler reconciles a MongoDBBackup object
type MongoDBBackupStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// TODO: better name
	Development bool
	Name        string
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstores/finalizers,verbs=update
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbbackupstorestores,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch,resourceNames=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *MongoDBBackupStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()

	// all errors we aggregate into this error
	errors := internalerrors.New()

	// measures all controller metrics at once, per iteration
	defer measureControllerReconciliation(r.Name, start, errors)

	log := log.FromContext(ctx)

	log.Info("reconciling backup store", "name", req.NamespacedName.String())

	var store airlockv1alpha1.MongoDBBackupStore

	// we defer to measure the status of the backup store after the reconciliation is complete
	// intermediate staged of ready vs not ready is not tracked
	defer updateBucketMetric(req.Name, req.Namespace, &store)

	err := r.Get(ctx, req.NamespacedName, &store)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, nil
	}

	statusMgr := conditions.NewManager(r, &store.Status.Conditions, &store, airlockv1alpha1.BackupStorePhaseRules)

	if store.Status.ObservedGeneration == nil {
		/*
		 * this is a fresh object, we perform some due dilligence to notify both the kubernetes server and the user about the start of the reconciliation process
		 * by setting all the conditions that need met for the resource to be in any known state (indicated by Phase)
		 */

		errors.Append(statusMgr.SetCondition(ctx, airlockv1alpha1.StoreConditionBucketExists, metav1.ConditionUnknown, airlockv1alpha1.StoreReasonBucketUnknown, "Bucket exists check has not been performed yet"))

		if errors.HasErrors() {
			return ctrl.Result{}, errors
		}
	}

	log.Info("validating bucket exists", "bucket", store.Spec.S3.Bucket)

	if err := r.validateBucketExists(ctx, &store); err != nil {
		log.Error(err, "failed to validate bucket exists")

		errors.Append(statusMgr.SetCondition(ctx,
			airlockv1alpha1.StoreConditionBucketExists,
			metav1.ConditionFalse,
			airlockv1alpha1.StoreReasonBucketNotExists,
			fmt.Sprintf("failed to validate bucket exists: %s", err.Error()),
		))

		return ctrl.Result{}, errors
	}

	// bucket exists
	errors.Append(statusMgr.SetCondition(ctx,
		airlockv1alpha1.StoreConditionBucketExists,
		metav1.ConditionTrue,
		airlockv1alpha1.StoreReasonBucketExists,
		"Store config successfully validated",
	))

	return ctrl.Result{}, errors.IfExists()
}

func updateBucketMetric(name, namespace string, store *airlockv1alpha1.MongoDBBackupStore) {
	if store == nil {
		metrics.RemoveBackupStore(namespace, name)
		return
	}

	if meta.IsStatusConditionTrue(store.Status.Conditions, airlockv1alpha1.StoreConditionBucketExists) {
		metrics.UpdateBackupStore(store.Namespace, store.Name, true)
	} else {
		metrics.UpdateBackupStore(store.Namespace, store.Name, false)
	}
}

func (r *MongoDBBackupStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// builds a map of secret name to backup store names
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &airlockv1alpha1.MongoDBBackupStore{}, "spec.s3.secretRef.name", func(rawObj client.Object) []string {
		backupStore := rawObj.(*airlockv1alpha1.MongoDBBackupStore)
		return []string{backupStore.Spec.S3.SecretRef.Name}
	}); err != nil {
		return fmt.Errorf("failed to index spec.s3.secretRef.name field: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBBackupStore{}).
		// allows for controller to always reflect the correct status of the backup store when the secret is updated
		// when a secret is updated, the controller will check what backup store CRs are mapped to this secret, and start reconciling them (which in this case just reflects the status of the store, i.e. the keys are valid or not)
		// references:
		// https://github.com/kubernetes-sigs/controller-runtime/blob/aebc15d7c68925a659ee8ae4a747802b7f87594f/pkg/client/example_test.go#L297-L298
		// https://buraksekili.github.io/articles/client-k8s-indexing/ better :" )
		//https://github.com/kubernetes-sigs/controller-runtime/issues/1941
		Watches(&v1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.getMappedBackupStore)).
		Complete(r)
}

type runtimeToAwslogger struct {
	logger logr.Logger
}

func (l runtimeToAwslogger) Logf(class logging.Classification, msg string, args ...any) {
	l.logger.WithValues("source", "aws", "class", class).Info(fmt.Sprintf(msg, args...))
}

func (r *MongoDBBackupStoreReconciler) validateBucketExists(ctx context.Context, store *airlockv1alpha1.MongoDBBackupStore) error {
	logger := log.FromContext(ctx)

	l := runtimeToAwslogger{
		logger: logger,
	}

	var secret v1.Secret
	secret.Name = store.Spec.S3.SecretRef.Name
	secret.Namespace = store.Namespace

	if err := r.Get(ctx, client.ObjectKeyFromObject(&secret), &secret); err != nil {
		return fmt.Errorf("failed to get S3 credentials secret: %w", err)
	}

	accessKeyData, exists := secret.Data[store.Spec.S3.SecretRef.Mappings.AccessKeyID.Key]
	if !exists {
		return fmt.Errorf("access key not found in secret at key: %s", store.Spec.S3.SecretRef.Mappings.AccessKeyID.Key)
	}

	secretKeyData, exists := secret.Data[store.Spec.S3.SecretRef.Mappings.SecretAccessKey.Key]
	if !exists {
		return fmt.Errorf("secret key not found in secret at key: %s", store.Spec.S3.SecretRef.Mappings.SecretAccessKey.Key)
	}

	accessKey := string(accessKeyData)
	secretKey := string(secretKeyData)

	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("S3 credentials are empty")
	}

	var httpClient *http.Client
	if r.Development {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}

	s3Client := s3.New(s3.Options{
		BaseEndpoint: &store.Spec.S3.Endpoint,
		Logger:       l,
		UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
			}, nil
		}),
		Region:     store.Spec.S3.Region,
		HTTPClient: httpClient,
	})

	if err := s3.NewBucketExistsWaiter(s3Client).Wait(ctx, &s3.HeadBucketInput{
		Bucket: &store.Spec.S3.Bucket,
	}, time.Second*10); err != nil {
		return fmt.Errorf("provided bucket could not be found %s", err.Error())
	}

	logger.Info("bucket exists", "bucket", store.Spec.S3.Bucket)

	return nil
}

func (r *MongoDBBackupStoreReconciler) getMappedBackupStore(ctx context.Context, object client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	logger.Info("getting mapped backup stores for secret", "name", object.GetName(), "namespace", object.GetNamespace())

	secret := object.(*v1.Secret)

	listOptions := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.s3.secretRef.name", secret.Name),
		Namespace:     secret.Namespace,
	}

	var storeList airlockv1alpha1.MongoDBBackupStoreList
	if err := r.List(ctx, &storeList, listOptions); err != nil {
		logger.Error(err, "failed to list secrets")
		return nil
	}

	var requests []reconcile.Request = make([]reconcile.Request, len(storeList.Items))
	for i, store := range storeList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      store.Name,
				Namespace: store.Namespace,
			},
		}
	}

	return requests
}
