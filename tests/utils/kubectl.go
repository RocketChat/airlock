package utils

import (
	"bytes"
	"os/exec"
)

type Kubectl struct {
	kubeConfig []byte

	namespace string
}

func NewKubectl(kubeConfig []byte) Kubectl {
	return Kubectl{kubeConfig: kubeConfig}
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
	return Kubectl{namespace: namespace, kubeConfig: k.kubeConfig}
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
