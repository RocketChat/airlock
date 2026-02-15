package controllers //chan type

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/RocketChat/airlock/api/v1alpha1"
	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/controllers/reconciler"
	"github.com/RocketChat/airlock/internal/conditions"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"go.mongodb.org/mongo-driver/bson"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getDatabaseSize is used to calculate the volume size required for the backup job
func getDatabaseSize(ctx context.Context, connectionString, database string) (int64, error) {
	logger := log.FromContext(ctx)

	logger.Info("trying to estimate db size", "database", database)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)

	db := client.Database(database)

	logger.Info("running dbStats against db", "database", database)

	// Run dbStats command to get database size
	var result bson.M
	err = db.RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&result)
	if err != nil {
		return 0, err
	}

	// Extract dataSize from the result
	dataSize, ok := result["dataSize"]
	if !ok {
		return 0, fmt.Errorf("dataSize not found in dbStats result")
	}

	logger.Info("dbSize response", "database", database, "size", dataSize)

	// Convert to int64 (dataSize can be int32 or int64)
	switch v := dataSize.(type) {
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(math.Ceil(v)), nil
	default:
		return 0, fmt.Errorf("unexpected dataSize type: %T", dataSize)
	}
}

func getMongoDbBackupImage(ctx context.Context, handler client.Client, cluster string) (string, error) {
	var clusterCr v1alpha1.MongoDBCluster

	err := handler.Get(ctx, client.ObjectKey{Name: cluster}, &clusterCr)
	if err != nil {
		return "", err
	}

	return clusterCr.Spec.BackupImage, nil
}

func reconcileMongoDbAccessRequest(ctx context.Context, cl client.Client, backupCr *v1alpha1.MongoDBBackup) (*v1alpha1.MongoDBAccessRequest, error) {
	var accessRequest v1alpha1.MongoDBAccessRequest

	accessRequest.Name = fmt.Sprintf("%s-access", backupCr.Name)

	accessRequest.Namespace = backupCr.Namespace

	_, err := reconciler.CreateOrPatch(ctx, cl, backupCr, &accessRequest, func() error {
		accessRequest.Spec.ClusterName = backupCr.Spec.Cluster
		accessRequest.Spec.Database = backupCr.Spec.Database
		accessRequest.Spec.UserName = backupCr.Name + "-user"

		return nil
	})

	return &accessRequest, err
}

func reconcilePvc(ctx context.Context, cl client.Client, statusMgr *conditions.ConditionsManager, backupCr v1alpha1.MongoDBBackup, accessRequest v1alpha1.MongoDBAccessRequest) (*v1.PersistentVolumeClaim, error) {
	logger := log.FromContext(ctx)

	var pvc v1.PersistentVolumeClaim

	pvc.Name = backupCr.Name
	pvc.Namespace = backupCr.Namespace

	_, err := reconciler.CreateOrPatch(ctx, cl, &backupCr, &pvc, func() error {
		exisingStorage := pvc.Spec.Resources.Requests.Storage()

		if exisingStorage.CmpInt64(0) == 0 {
			logger.Info("no existing request set, requesting db size")
			// get new size and set it
			if err := wait.PollUntilContextTimeout(ctx, time.Second, time.Minute*3, false, func(ctx context.Context) (done bool, err error) {
				if err := cl.Get(ctx, client.ObjectKeyFromObject(&accessRequest), &accessRequest); err != nil {
					return false, err
				}

				if meta.IsStatusConditionTrue(accessRequest.Status.Conditions, "Ready") {
					return true, nil
				}

				return false, nil
			}); err != nil {
				return err
			}

			if err := statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionAccessRequestReady, metav1.ConditionTrue, airlockv1alpha1.BackupReasonAccessRequestReady, "Access request is ready"); err != nil {
				return err
			}

			var secret v1.Secret

			secret.Name = accessRequest.Spec.SecretName
			if secret.Name == "" {
				secret.Name = accessRequest.Name
			}

			secret.Namespace = accessRequest.Namespace

			if err := cl.Get(ctx, client.ObjectKeyFromObject(&secret), &secret); err != nil {
				return err
			}

			size, err := getDatabaseSize(ctx, string(secret.Data["connectionString"]), backupCr.Spec.Database)
			if err != nil {
				return err
			}

			// add some buffer (2x the database size) for backup overhead and splitting
			requestSize := max(size*2, 1024*1024*1024)

			pvc.Spec = v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				},
				Resources: v1.VolumeResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: *resource.NewQuantity(requestSize, resource.BinarySI),
					},
				},
				StorageClassName: nil,
			}
		} else {
			// mmake sure we keep thi9s
			pvc.Spec.Resources.Requests = v1.ResourceList{
				v1.ResourceStorage: *pvc.Spec.Resources.Requests.Storage(),
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &pvc, nil
}

func _getEnvsForMongo(accessRequest v1alpha1.MongoDBAccessRequest, backup v1alpha1.MongoDBBackup) []v1.EnvVar {
	return []v1.EnvVar{
		getEnvVarFromSecret("MONGODB_URI", accessRequest.Name, "connectionString"),
		getEnvVar("COLLECTIONS", strings.Join(backup.Spec.IncludedCollections, ",")),
		getEnvVar("EXCLUDED_COLLECTIONS", strings.Join(backup.Spec.ExcludedCollections, ",")),
		getEnvVar("DATABASE", backup.Spec.Database),
	}
}

func _getS3EnvVars(ctx context.Context, cl client.Client, backupCr v1alpha1.MongoDBBackup) ([]v1.EnvVar, error) {
	var store = v1alpha1.MongoDBBackupStore{}

	store.Name = backupCr.Spec.BackupStoreRef.Name
	store.Namespace = backupCr.Spec.BackupStoreRef.Namespace

	err := cl.Get(ctx, client.ObjectKeyFromObject(&store), &store)
	if err != nil {
		return []v1.EnvVar{}, err
	}

	return []v1.EnvVar{
		getEnvVar("AWS_ENDPOINT_URL_S3", store.Spec.S3.Endpoint),
		getEnvVar("AWS_REGION", store.Spec.S3.Region),
		getEnvVarFromSecret("AWS_ACCESS_KEY_ID", store.Spec.S3.SecretRef.Name, store.Spec.S3.SecretRef.Mappings.AccessKeyID.Key),
		getEnvVarFromSecret("AWS_SECRET_ACCESS_KEY", store.Spec.S3.SecretRef.Name, store.Spec.S3.SecretRef.Mappings.SecretAccessKey.Key),
		getEnvVar("BUCKET", store.Spec.S3.Bucket),
	}, nil
}

func _reconcileJob(ctx context.Context, cl client.Client, statusMgr *conditions.ConditionsManager, backupCr *v1alpha1.MongoDBBackup, mode string) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	// use backup job for the image
	image, err := getMongoDbBackupImage(ctx, cl, backupCr.Spec.Cluster)
	if err != nil {
		return nil, err
	}

	accessRequest, err := reconcileMongoDbAccessRequest(ctx, cl, backupCr)
	if err != nil {
		return nil, err
	}

	pvc, err := reconcilePvc(ctx, cl, statusMgr, *backupCr, *accessRequest)
	if err != nil {
		return nil, err
	}

	s3EnvVars, err := _getS3EnvVars(ctx, cl, *backupCr)
	if err != nil {
		return nil, err
	}

	mongoEnvVars := _getEnvsForMongo(*accessRequest, *backupCr)

	jobEnvVars := append(append(mongoEnvVars, s3EnvVars...), getEnvVar("PREFIX", backupCr.Spec.Prefix), getEnvVar("NO_VERIFY_SSL", "true"), getEnvVar("BACKUP_FILE", "/backups/backup.gz"))

	if backupCr.Spec.Encrypt.Enabled {
		logger.Info("encryption enabled", "engine", backupCr.Spec.Encrypt.Engine)

		// we don't care about loading the secret here
		if err := cl.Get(ctx, client.ObjectKey{Name: backupCr.Spec.Encrypt.AgeSecretRef.Name, Namespace: backupCr.Spec.Encrypt.AgeSecretRef.Namespace}, &v1.Secret{}); err != nil {
			errors := internalerrors.New()

			errors.Append(err)

			logger.Error(err, "failed to get age secret, not scheduling backup", "name", backupCr.Spec.Encrypt.AgeSecretRef.Name, "namespace", backupCr.Spec.Encrypt.AgeSecretRef.Namespace)
			if err := statusMgr.SetCondition(ctx, airlockv1alpha1.BackupConditionJobScheduled, metav1.ConditionFalse, "AgeSecretNotFound", "Age secret not found"); err != nil {
				return nil, errors.Append(err)
			}

			return nil, errors
		}

		ageEnvVar := getEnvVarFromSecret("AGE_PRIVATE_KEYS", backupCr.Spec.Encrypt.AgeSecretRef.Name, backupCr.Spec.Encrypt.AgeSecretRef.Mapping.Key)

		jobEnvVars = append(jobEnvVars, ageEnvVar)
	}

	var job = batchv1.Job{}

	job.Name = backupCr.Name
	job.Namespace = backupCr.Namespace

	_, err = reconciler.CreateOrPatch(ctx, cl, backupCr, &job, func() error {
		container := v1.Container{
			Image:           image,
			ImagePullPolicy: v1.PullIfNotPresent,
			Args:            []string{mode},
			Name:            backupCr.Name,
			Env:             jobEnvVars,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "backup-storage",
					MountPath: "/backups",
				},
			},
		}

		job.Spec.Template.Spec.Containers = []v1.Container{container}
		job.Spec.Template.Spec.Volumes = []v1.Volume{
			{
				Name: "backup-storage",
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc.Name,
					},
				},
			},
		}

		job.Spec.Template.Spec.RestartPolicy = v1.RestartPolicyNever
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &job, nil
}

func reconcileBackupJob(ctx context.Context, cl client.Client, statusMgr *conditions.ConditionsManager, backupCr *v1alpha1.MongoDBBackup) (*batchv1.Job, error) {
	return _reconcileJob(ctx, cl, statusMgr, backupCr, "backup")
}

func reconcileRestoreJob(ctx context.Context, cl client.Client, statusMgr *conditions.ConditionsManager, backupCr *v1alpha1.MongoDBBackup) (*batchv1.Job, error) {
	return _reconcileJob(ctx, cl, statusMgr, backupCr, "restore")
}
