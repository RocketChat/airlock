package controllers

import (
	"github.com/mongodb-forks/digest"
	"go.mongodb.org/atlas/mongodbatlas"
	corev1 "k8s.io/api/core/v1"
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
