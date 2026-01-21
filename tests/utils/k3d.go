package utils

import (
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
	return Make("k3d-cluster", MakeVar("NAME", k.name))
}

func (k K3dCluster) Stop() error {
	return RunStreamOutput("k3d", "cluster", "stop", k.name)
}

func (k K3dCluster) Delete() error {
	return RunStreamOutput("k3d", "cluster", "delete", k.name)
}

func (k K3dCluster) LoadImage(image string) error {
	return RunStreamOutput("k3d", "image", "import", "-c", k.name, image)
}

func (k K3dCluster) DeployMongo() error {
	return Make("k3d-deploy-mongo", MakeVar("NAME", k.name))
}

func (k K3dCluster) DeployMinio() error {
	return Make("k3d-deploy-minio", MakeVar("NAME", k.name))
}

func (k K3dCluster) DeployAirlock() error {
	return Make("k3d-deploy-airlock", MakeVar("NAME", k.name), MakeVar("IMG", "controller:latest"))
}

func (k K3dCluster) ApplyMongodbBackupStore() error {
	return Make("k3d-add-backup-store", MakeVar("NAME", k.name))
}

func (k K3dCluster) LoadSampleDataToMongo() error {
	return Make("k3d-load-mongo-data", MakeVar("NAME", k.name))
}

func (k K3dCluster) LoadBackupImage() error {
	return Make("k3d-load-backup-image", MakeVar("NAME", k.name))
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
