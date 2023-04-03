package controllers

import (
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
