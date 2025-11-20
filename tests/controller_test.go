package tests

import (
	"fmt"
	"os"
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

		By("installing mongo namespace")
		Expect(kubectl.CreateNamespace("mongo")).ToNot(HaveOccurred())

		mongoImage := os.Getenv("LOAD_MONGO_FROM_LOCAL")
		if mongoImage != "" {
			By("loading mongo image from local")
			Expect(cluster.LoadImage(mongoImage)).ToNot(HaveOccurred())
		}

		By("installing mongodb pod and service")
		Expect(kubectl.Apply(filepath.Join("assets", "mongo"))).ToNot(HaveOccurred())

		getPodStatus := func() error {
			output, err := kubectl.WithNamespace("mongo").GetPods("-l", "app=mongo", "-o", "jsonpath={.items[*].status}")
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

	// AfterAll(func() {
	// 	By("Deleting the namespace")
	// 	Expect(kubectl.DeleteNamespace(namespace)).ToNot(HaveOccurred())
	// })

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

		It("should mark cluster resource as ready", func() {
			By("applying mongodb cluster resources")
			Expect(kubectl.Apply(filepath.Join("assets", "airlock"))).ToNot(HaveOccurred())

			EventuallyWithOffset(1, func() error {
				output, err := kubectl.Get("mongodbcluster", "airlock-test", "-o", "jsonpath='{.status.conditions[].type}'")
				if err != nil {
					return err
				}

				fmt.Printf("MongoDBCluster Status: %s\n", output)

				if string(output) != "'Ready'" {
					return fmt.Errorf("mongodbcluster not yet in ready state: %s", output)
				}

				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
