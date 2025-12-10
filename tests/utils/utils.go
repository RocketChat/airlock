package utils

import (
	"fmt"
	"os"
	"os/exec"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"

	//nolint:revive
	//nolint:golint
	. "github.com/onsi/gomega"
)

func getRootDir() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	// remove the \n before returning
	return string(output[:len(output)-1]), err
}

func Run(cmd ...string) ([]byte, error) {
	root, err := getRootDir()
	if err != nil {
		return nil, err
	}

	command := exec.Command(cmd[0], cmd[1:]...)

	command.Dir = root

	return runCommand(command)
}

func RunStreamOutput(cmd ...string) error {
	root, err := getRootDir()
	if err != nil {
		return err
	}

	command := exec.Command(cmd[0], cmd[1:]...)

	command.Dir = root

	command.Stdout = os.Stdout

	fmt.Fprintf(GinkgoWriter, "running: %s\n", command.String())

	err = command.Run()
	if err != nil {
		return fmt.Errorf("%s failed with error: %v", command, err)
	}

	return nil
}

func runCommand(command *exec.Cmd) ([]byte, error) {
	fmt.Fprintf(GinkgoWriter, "running: %s\n", command.String())
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}

func ExpectRunToHaveSucceeded(cmd ...string) []byte {
	output, err := Run(cmd...)

	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return output
}

func BuildImage(imageName string) {
	_, err := Run("make", "build-docker-no-test", fmt.Sprintf("IMG=%s", imageName))

	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

func MakeVar(variable, value string) string {
	return fmt.Sprintf("%s=%s", variable, value)
}
