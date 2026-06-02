package controllers

import (
	"context"
	"strings"

	"github.com/mongodb-forks/digest"
	"go.mongodb.org/atlas-sdk/v20250312020/admin"
	"go.mongodb.org/atlas/mongodbatlas"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
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

func getAtlasAdminClientFromSecret(secret *corev1.Secret) (*admin.APIClient, string, error) {
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

	client, err := admin.NewClient(admin.UseDigestAuth(atlasPublicKey, atlasPrivateKey))
	if err != nil {
		return nil, "", err
	}

	return client, atlasGroupID, nil
}

func resolveAtlasClusterName(ctx context.Context, spec airlockv1alpha1.MongoDBClusterSpec, client *mongodbatlas.Client, groupID string) (string, error) {
	if spec.AtlasClusterName != "" {
		return spec.AtlasClusterName, nil
	}

	if spec.HostTemplate != "" {
		return getClusterNameFromHostTemplate(ctx, client, groupID, spec.HostTemplate)
	}

	return "", errors.NewBadRequest("atlasClusterName or hostTemplate is required")
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
