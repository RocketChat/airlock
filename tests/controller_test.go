package tests

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/gomega"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
)

const namespace = "airlock-system"

const accessRequestName = "test-request"

var _ = Describe("airlock", Ordered, func() {
	BeforeAll(func() {
		By("Creating the namespace")
		Expect(kubectl.CreateNamespaceIfNotExists(namespace)).ToNot(HaveOccurred())

		By("applying RBAC")
		Expect(kubectl.KApply(filepath.Join("..", "config", "rbac"))).ToNot(HaveOccurred())

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

		It("should create mongo user as per access request", func() {
			accessRequestResource := &airlockv1alpha1.MongoDBAccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      accessRequestName,
					Namespace: "mongo",
				},

				Spec: airlockv1alpha1.MongoDBAccessRequestSpec{
					Database:    "test",
					ClusterName: "airlock-test",
					SecretName:  "test-access-secret",
				},
			}

			err := k8sClient.Create(context.Background(), accessRequestResource)
			Expect(err).ToNot(HaveOccurred())

			// next we need to wait for the user to have been created
			EventuallyWithOffset(1, func() error {
				accessRequest := airlockv1alpha1.MongoDBAccessRequest{}

				err = k8sClient.Get(context.Background(), client.ObjectKey{Name: accessRequestName, Namespace: "mongo"}, &accessRequest)
				if err != nil {
					return err
				}

				ready := false

				// TODO: i doubt this is full proof
				for _, condition := range accessRequest.Status.Conditions {
					if condition.Type == "Ready" {
						ready = true
						break
					}
				}

				if !ready {
					return fmt.Errorf("access request not yet ready")
				}

				var secret v1.Secret

				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name:      accessRequestResource.Spec.SecretName,
					Namespace: "mongo",
				}, &secret)

				if err != nil {
					return err
				}

				_, hasConnectionString := secret.Data["connectionString"]
				if !hasConnectionString {
					return fmt.Errorf("generated secret is missing connectionSecret")
				}

				_, hasPassword := secret.Data["password"]
				if !hasPassword {
					return fmt.Errorf("generated secret is missing password")
				}

				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})

		It("should delete the access secret if the corresponding accessrequest is deleted", func() {
			accessRequestResource := &airlockv1alpha1.MongoDBAccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      accessRequestName,
					Namespace: "mongo",
				},

				Spec: airlockv1alpha1.MongoDBAccessRequestSpec{
					Database:    "test",
					ClusterName: "airlock-test",
					SecretName:  "test-access-secret",
				},
			}

			err := k8sClient.Delete(context.Background(), accessRequestResource)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				var secret v1.Secret

				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name:      accessRequestResource.Spec.SecretName,
					Namespace: "mongo",
				}, &secret)

				if err == nil {
					return fmt.Errorf("secret hasn't been deleted yet")
				}

				if !errors.IsNotFound(err) {
					return fmt.Errorf("failed to try to fetch secret: %v", err)
				}

				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})

		It("should create and manage MongoDBBackup", func() {
			backupName := "test-backup"
			backup := &airlockv1alpha1.MongoDBBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backupName,
					Namespace: "mongo",
				},
				Spec: airlockv1alpha1.MongoDBBackupSpec{
					MongoDBRef: airlockv1alpha1.MongoDBRef{
						Name:      "mongo",
						Namespace: "default",
					},
					Namespaces: []airlockv1alpha1.MongoDBNamespace{
						{
							Database:    "test",
							Collections: []string{"users"},
						},
					},
					Storage: airlockv1alpha1.MongoDBBackupStorage{
						Type: "s3",
						S3: &airlockv1alpha1.MongoDBBackupS3{
							Endpoint: "s3.amazonaws.com",
							Bucket:   "test-bucket",
							Region:   "us-east-1",
							SecretRef: airlockv1alpha1.S3SecretRef{
								Name: "s3-credentials",
								Key:  "credentials",
							},
						},
					},
				},
			}

			By("Creating MongoDBBackup")
			err := k8sClient.Create(context.Background(), backup)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying backup is created")
			var fetchedBackup airlockv1alpha1.MongoDBBackup
			err = k8sClient.Get(context.Background(), client.ObjectKey{
				Name: backupName, Namespace: "mongo",
			}, &fetchedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(fetchedBackup.Spec.MongoDBRef.Name).To(Equal("mongo"))
		})
	})
})
