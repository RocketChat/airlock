package controllers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/conditions"
	"github.com/RocketChat/airlock/internal/config"
	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/RocketChat/portmaster-v2/pkg/manifest"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/go-logr/logr"
	"github.com/mongodb-forks/digest"
	"go.mongodb.org/atlas/mongodbatlas"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func getSecretProperty(secret *v1.Secret, property string) (string, error) {
	value := string(secret.Data[property])
	if value == "" {
		err := errors.NewServiceUnavailable(property + " not found in secret " + secret.Name)
		return value, err
	}

	return value, nil
}

func getAtlasClientFromSecret(secret *v1.Secret) (*mongodbatlas.Client, string, error) {
	atlasPublicKey, err := getSecretProperty(secret, "atlasPublicKey")
	if err != nil {
		return nil, "", err
	}

	atlasPrivateKey, err := getSecretProperty(secret, "atlasPrivateKey")
	if err != nil {
		return nil, "", err
	}

	atlasGroupID, err := getSecretProperty(secret, "atlasGroupID")
	if err != nil {
		return nil, "", err
	}

	t := digest.NewTransport(atlasPublicKey, atlasPrivateKey)

	tc, err := t.Client()
	if err != nil {
		return nil, "", err
	}

	client := mongodbatlas.NewClient(tc)

	return client, atlasGroupID, nil
}

func getClusterNameFromHostTemplate(ctx context.Context, client *mongodbatlas.Client, groupID, hostTemplate string) (string, error) {
	clusters, _, err := client.Clusters.List(ctx, groupID, &mongodbatlas.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, cluster := range clusters {
		// Check for the host template in both SrvAddress (mongo+srv:// connection) and MongoURI (legacy replicaset uri with the 3 RS menbers)
		if strings.Contains(cluster.SrvAddress, hostTemplate) {
			return cluster.Name, nil
		}

		if strings.Contains(cluster.MongoURI, hostTemplate) {
			return cluster.Name, nil
		}
	}

	return "", errors.NewBadRequest("Cluster not found when searching for it's connectionString in atlas")
}

func measureControllerReconciliation(name string, start time.Time, errors *internalerrors.AggregateError) {
	metrics.ObserveControllerReconcileDuration(name, time.Since(start))

	if errors.HasErrors() {
		metrics.IncControllerError(name)
	} else {
		metrics.IncControllerSuccess(name)
	}
}

func hasJobCompleted(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

func hasJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

type runtimeToAwslogger struct {
	logger logr.Logger
}

func (l runtimeToAwslogger) Logf(class logging.Classification, msg string, args ...any) {
	l.logger.WithValues("source", "aws", "class", class).Info(fmt.Sprintf(msg, args...))
}

func newS3Client(ctx context.Context, region, accessKeyId, secretAccessKey string, ignoreTls bool) *s3.Client {
	logger := log.FromContext(ctx)

	l := runtimeToAwslogger{
		logger: logger,
	}

	s3Options := s3.Options{
		Logger:       l,
		UsePathStyle: true,
		Region:       region,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKeyId,
				SecretAccessKey: secretAccessKey,
			}, nil
		}),
	}

	if ignoreTls {
		s3Options.HTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}

	return s3.New(s3Options)
}

func getS3PropertiesFromSecret(secret *v1.Secret) (bucket string, region string, accessKeyId string, secretAccessKey string, err error) {
	accessKeyId, err = getSecretProperty(secret, airlockv1alpha1.DestinationBucketSecretRefAccessKeyID)
	if err != nil {
		return "", "", "", "", err
	}

	secretAccessKey, err = getSecretProperty(secret, airlockv1alpha1.DestinationBucketSecretRefSecretAccessKey)
	if err != nil {
		return "", "", "", "", err
	}

	region, err = getSecretProperty(secret, airlockv1alpha1.DestinationBucketSecretRefRegion)
	if err != nil {
		return "", "", "", "", err
	}

	bucket, err = getSecretProperty(secret, airlockv1alpha1.DestinationBucketSecretRefBucket)
	if err != nil {
		return "", "", "", "", err
	}

	return
}

func validateBucketExists(ctx context.Context, statusMgr *conditions.ConditionsManager, secret *v1.Secret, config *config.Config) error {
	logger := log.FromContext(ctx)

	bucket, region, accessKeyId, secretAccessKey, err := getS3PropertiesFromSecret(secret)
	if err != nil {
		return fmt.Errorf("failed to get S3 properties from secret: %w", err)
	}

	logger.Info("validating bucket exists", "bucket", bucket, "region", region)

	timeout, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("context has no deadline")
	}

	s3Client := newS3Client(ctx, region, accessKeyId, secretAccessKey, config.BackupConfig.IgnoreTls)

	if err := s3.NewBucketExistsWaiter(s3Client).Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, time.Until(timeout)); err != nil {
		return fmt.Errorf("provided bucket could not be found %w", err)
	}

	logger.Info("bucket exists", "bucket", bucket)

	return nil
}

func waitUntilAccessRequestIsReady(ctx context.Context, c client.Client, accessRequest *airlockv1alpha1.MongoDBAccessRequest) error {
	logger := log.FromContext(ctx)
	if err := wait.PollUntilContextCancel(ctx, time.Second*5, true, func(ctx context.Context) (done bool, err error) {
		logger.Info("waiting for access request to be ready")

		var obj airlockv1alpha1.MongoDBAccessRequest
		if err := c.Get(ctx, client.ObjectKeyFromObject(accessRequest), &obj); err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(obj.Status.Conditions, airlockv1alpha1.ConditionReady) {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return err
	}

	return nil
}

func getDatabaseSizeFromAccessRequest(ctx context.Context, c client.Client, accessRequest *airlockv1alpha1.MongoDBAccessRequest) (int64, error) {
	secret := &v1.Secret{}
	secret.Name = accessRequest.Spec.SecretName
	secret.Namespace = accessRequest.Namespace
	if err := c.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		err := fmt.Errorf("failed to get secret: %w", err)
		return 0, err
	}

	connectionString, err := getSecretProperty(secret, "connectionString")
	if err != nil {
		err := fmt.Errorf("failed to get connection string: %w", err)
		return 0, err
	}

	return getDatabaseSize(ctx, connectionString, accessRequest.Spec.Database)
}

func getVolumeSizeFromDestinationBucketManifest(ctx context.Context, bucket, prefix, region, accessKeyId, secretAccessKey, manifestName string) (uint64, error) {
	s3Client := newS3Client(ctx, region, accessKeyId, secretAccessKey, false)

	file := filepath.Join(
		prefix,
		manifestName,
	)

	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(file),
	})
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	m, err := manifest.Load(resp.Body)
	if err != nil {
		return 0, err
	}

	return m.EstimatedDiskUsage(), nil
}
