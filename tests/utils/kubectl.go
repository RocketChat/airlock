package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Kubectl struct {
	kubeConfig []byte

	namespace string

	k8sClient client.Client
}

func NewKubectl(kubeConfig []byte) Kubectl {
	return Kubectl{kubeConfig: kubeConfig}
}

func (k *Kubectl) SetK8sClient(c client.Client) {
	k.k8sClient = c
}

func (k Kubectl) getArgs(cmd []string) []string {
	args := []string{"--kubeconfig=/dev/fd/0"}

	if k.namespace != "" {
		args = append(args, "-n", k.namespace)
	}

	args = append(args, cmd...)

	return args
}

func (k Kubectl) getCommand(cmd []string) *exec.Cmd {
	command := exec.Command("kubectl", k.getArgs(cmd)...)
	return command
}

func (k Kubectl) run(cmd []string) ([]byte, error) {
	command := k.getCommand(cmd)

	command.Stdin = bytes.NewBuffer(k.kubeConfig)

	return runCommand(command)
}

func (k Kubectl) WithNamespace(namespace string) Kubectl {
	return Kubectl{namespace: namespace, kubeConfig: k.kubeConfig, k8sClient: k.k8sClient}
}

func (k Kubectl) Apply(file string) error {
	_, err := k.run([]string{"apply", "-f", file})
	return err
}

func (k Kubectl) KApply(file string) error {
	_, err := k.run([]string{"apply", "-k", file})
	return err
}

func (k Kubectl) GetPods(args ...string) ([]byte, error) {
	return k.run(append([]string{"get", "pods"}, args...))
}

func (k Kubectl) GetAllDeployments() ([]byte, error) {
	return k.run([]string{"get", "deployments"})
}

func (k Kubectl) DescribeDeployment(name string) ([]byte, error) {
	return k.run([]string{"describe", "deployment", name})
}

func (k Kubectl) CreateNamespaceIfNotExists(name string) error {
	_, err := k.Get("namespace", name)
	if err != nil {
		_, err := k.run([]string{"create", "namespace", name})
		return err
	}

	return nil
}

func (k Kubectl) CreateNamespace(name string) error {
	_, err := k.run([]string{"create", "namespace", name})
	return err
}

func (k Kubectl) DeleteNamespace(name string) error {
	_, err := k.run([]string{"delete", "namespace", name})
	return err
}

func (k Kubectl) Get(args ...string) ([]byte, error) {
	return k.run(append([]string{"get"}, args...))
}

var noK8sClient = errors.New("k8sClient not found")

// TODO: maybe move to diff struct
func (k Kubectl) isAnyPodReadyNative(pod string, selector map[string]string) error {
	if k.k8sClient == nil {
		fmt.Println("no k8sClient is set")
		return noK8sClient
	}

	podList := &corev1.PodList{}
	listOptions := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(selector),
	}

	if k.namespace != "" {
		listOptions.Namespace = k.namespace
	}

	err := k.k8sClient.List(context.Background(), podList, listOptions)
	if err != nil {
		return err
	}

	// Check if any pods are ready
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return nil
		}
	}

	return fmt.Errorf("no pods are ready for %s", pod)
}
func (k Kubectl) IsAnyPodReady(pod string, selector map[string]string) error {
	if err := k.isAnyPodReadyNative(pod, selector); !errors.Is(err, noK8sClient) {
		return err
	}

	selectorStrings := []string{}
	for label, value := range selector {
		selectorStrings = append(selectorStrings, fmt.Sprintf("%s=%s", label, value))
	}

	output, err := k.GetPods("-l", strings.Join(selectorStrings, ","), "-o", "jsonpath={.items[*].status}")

	if len(output) > 0 {
		fmt.Println(string(output))
	}

	if err != nil {
		return err
	}

	if !strings.Contains(string(output), "\"phase\":\"Running\"") {
		return fmt.Errorf("%s pod in %s status", pod, output)
	}

	return nil
}
