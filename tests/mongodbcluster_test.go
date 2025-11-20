package tests

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"
	//nolint:golint
	//nolint:revive
	. "github.com/onsi/gomega"
)

const namespace = "airlock-system"

var _ = Describe("airlock", Ordered, func() {
	BeforeAll(func() {
		By("Creating the namespace")
		Expect(kubectl.CreateNamespace(namespace)).ToNot(HaveOccurred())

		By("applying RBAC")
		Expect(kubectl.KApply(filepath.Join("..", "config", "rbac"))).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		By("Deleting the namespace")
		Expect(kubectl.DeleteNamespace(namespace)).ToNot(HaveOccurred())
	})

	Context("Airlock Controller", func() {
		It("should run successfully", func() {
			// FIXME: this is failig -_-
			// utils.BuildImage("controller:latest")

			By("deploying airlock")
			err := kubectl.Apply(filepath.Join("..", "config", "manager", "manager.yaml"))

			Expect(err).NotTo(HaveOccurred())

			By("validating pod status phase=running")
			getPodStatus := func() error {
				output, err := kubectl.WithNamespace(namespace).GetPods("-l", "app.kubernetes.io/name=airlock", "-o", "jsonpath={.items[*].status}")
				if len(output) > 0 {
					fmt.Println(string(output))
				}
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if !strings.Contains(string(output), "\"phase\":\"Running\"") {
					return fmt.Errorf("airlock pod in %s status", output)
				}

				return nil
			}

			EventuallyWithOffset(1, getPodStatus, time.Minute, time.Second).Should(Succeed())
		})
	})
})
