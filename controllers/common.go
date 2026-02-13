package controllers

import (
	"context"
	"strings"
	"time"

	internalerrors "github.com/RocketChat/airlock/internal/errors"
	"github.com/RocketChat/airlock/internal/metrics"
	"github.com/mongodb-forks/digest"
	"go.mongodb.org/atlas/mongodbatlas"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

func getSecretProperty(secret *corev1.Secret, property string) (string, error) {
	value := string(secret.Data[property])
	if value == "" {
		err := errors.NewServiceUnavailable(property + " not found in secret " + secret.Name)
		return value, err
	}

	return value, nil
}

func getAtlasClientFromSecret(secret *corev1.Secret) (*mongodbatlas.Client, string, error) {
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

type PhaseType string

type ConditionType string

func measureControllerReconciliation(name string, start time.Time, errors *internalerrors.AggregateError) {
	metrics.ObserveControllerReconcileDuration(name, time.Since(start))

	if errors.HasErrors() {
		metrics.IncControllerError(name)
	} else {
		metrics.IncControllerSuccess(name)
	}
}
