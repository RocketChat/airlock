package utils

import (
	"fmt"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type K3dCluster struct {
	name string
}

func NewK3dCluster(name string) K3dCluster {
	return K3dCluster{name}
}

func (k K3dCluster) Start() error {
	// stdout, err := Run("k3d", "cluster", "create", k.name, "--kubeconfig-update-default=false", "--kubeconfig-switch-context=false", "--no-lb", "--no-rollback", "--wait", "-s1", "-a1")
	stdout, err := Run("make", "k3d-cluster")
	fmt.Println(string(stdout))
	return err
}

func (k K3dCluster) Stop() error {
	_, err := Run("k3d", "cluster", "stop", k.name)
	return err
}

func (k K3dCluster) Delete() error {
	_, err := Run("k3d", "cluster", "delete", k.name)
	return err
}

func (k K3dCluster) LoadImage(image string) error {
	_, err := Run("k3d", "image", "import", "-c", k.name, image)
	return err
}

func (k K3dCluster) Kubeconfig() ([]byte, error) {
	kubeconfig, err := Run("k3d", "kubeconfig", "show", k.name)
	if err != nil {
		return nil, err
	}

	return kubeconfig, nil
}

func (k K3dCluster) Kubectl() (*Kubectl, error) {
	kubeconfig, err := k.Kubeconfig()
	if err != nil {
		return nil, err
	}

	kk := NewKubectl(kubeconfig)

	return &kk, nil
}

func (k K3dCluster) K8sClient() (*client.Client, error) {
	kubeconfig, err := k.Kubeconfig()
	if err != nil {
		return nil, err
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	err = airlockv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	err = corev1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}
	err = batchv1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	k8s, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})

	return &k8s, err
}
