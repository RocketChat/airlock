package utils

import (
	"fmt"
	"os/exec"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"

	//nolint:revive
	//nolint:golint
	. "github.com/onsi/gomega"
)

func Run(cmd ...string) ([]byte, error) {
	command := exec.Command(cmd[0], cmd[1:]...)
	return runCommand(command)
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
