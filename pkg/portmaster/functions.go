package portmaster

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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

func validateBucketExists(ctx context.Context, client *s3.Client, bucket string) error {
	timeout, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("context has no deadline")
	}

	if err := s3.NewBucketExistsWaiter(client).Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, time.Until(timeout)); err != nil {
		return fmt.Errorf("provided bucket could not be found %w", err)
	}

	return nil
}

func castMapValueToString(m map[string][]byte, key string) (string, bool) {
	value, ok := m[key]
	return string(value), ok
}
