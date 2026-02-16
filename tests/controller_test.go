package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/gomega"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/tests/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
)

const namespace = "airlock-system"

const accessRequestName = "test-request"

const backupStoreName = "mongodbbackupstore-sample"

const mongoNamespace = "mongo"

const backupStoreSecretName = "mongodbbucketstoresecret"

const backupName = "test-backup"

var validBackupSpec = &airlockv1alpha1.MongoDBBackup{
	ObjectMeta: metav1.ObjectMeta{
		Name:      backupName,
		Namespace: mongoNamespace,
	},
	Spec: airlockv1alpha1.MongoDBBackupSpec{
		Cluster:             "airlock-test",
		Database:            "sample_training",
		ExcludedCollections: []string{},
		IncludedCollections: []string{},
		Prefix:              "test-prefix",
		BackupStoreRef: airlockv1alpha1.MongoDBBackupStoreRef{
			Name:      backupStoreName,
			Namespace: mongoNamespace,
		},
		Encryption: airlockv1alpha1.MongoDBBackupEncryption{
			Enabled: true,
			Engine:  "age",
			AgeSecretRef: airlockv1alpha1.MongoDBEncryptionAgeSecretRef{
				Name:      "age-encryption-key",
				Namespace: mongoNamespace,
				Mapping:   airlockv1alpha1.ToKeyMap{Key: "age-key"},
			},
		},
	},
}

const scheduleName = "test-backup-schedule"

var suspend = false

var validScheduleSpec = &airlockv1alpha1.MongoDBBackupSchedule{
	ObjectMeta: metav1.ObjectMeta{
		Name:      scheduleName,
		Namespace: mongoNamespace,
	},
	Spec: airlockv1alpha1.MongoDBBackupScheduleSpec{
		Schedule:   "*/1 * * * *",
		BackupSpec: validBackupSpec.Spec,
		Suspend:    &suspend,
	},
}

var validRestore = &airlockv1alpha1.MongoDBRestore{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-restore",
		Namespace: mongoNamespace,
	},
	Spec: airlockv1alpha1.MongoDBRestoreSpec{
		Cluster:      "airlock-test",
		Database:     "sample_training",
		DropDatabase: true,
		S3Path:       "test.archive",
		BackupStoreRef: airlockv1alpha1.MongoDBBackupStoreRef{
			Name:      backupStoreName,
			Namespace: mongoNamespace,
		},
	},
}

const (
	PhaseRunning   = "Running"
	PhasePending   = "Pending"
	PhaseFailed    = "Failed"
	PhaseCompleted = "Completed"
	PhaseReady     = "Ready"
	PhaseNotReady  = "NotReady"
)

var _ = Describe("Airlock Controller", Ordered, func() {
	BeforeAll(func() {
		By("Creating the namespace")
		Expect(kubectl.CreateNamespaceIfNotExists(namespace)).ToNot(HaveOccurred())

		By("applying RBAC")
		Expect(kubectl.KApply(filepath.Join("..", "config", "rbac"))).ToNot(HaveOccurred())

		By("validating mongo is running")
		Eventually(func() error {
			return kubectl.WithNamespace("mongo").IsAnyPodReady("mongo", map[string]string{"app": "mongo"})
		}, time.Minute, time.Second).Should(Succeed())

		By("validating airlock is running")
		Eventually(func() error {
			return kubectl.WithNamespace("airlock-system").IsAnyPodReady("airlock", map[string]string{"app.kubernetes.io/name": "airlock"})
		}, time.Minute, time.Second).Should(Succeed())
	})

	// AfterAll(func() {
	// 	By("Deleting the namespace")
	// 	Expect(kubectl.DeleteNamespace(namespace)).ToNot(HaveOccurred())
	// })

	Context("MongoDBCluster", func() {
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

		Context("MongoDBAccessRequest", func() {
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
		})

		Context("MongodbBackupStoreController", func() {
			var storeSecretData map[string][]byte

			It("should check state of store positively", func() {
				Expect(cluster.ApplyMongodbBackupStore()).ToNot(HaveOccurred())

				By("Eventually store status should be Ready")

				Eventually(func() (string, error) {
					store := &airlockv1alpha1.MongoDBBackupStore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backupStoreName,
						Namespace: mongoNamespace,
					}, store)

					if err != nil {
						return "", err
					}

					return store.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseReady))
			})

			It("should check state of store negatively", func() {
				By("Eventually store status should be NotReady")

				// update secret to have invalid credentials
				secret := &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      backupStoreSecretName,
						Namespace: mongoNamespace,
					},
					Data: map[string][]byte{
						"accessKeyId":     []byte("invalid"),
						"secretAccessKey": []byte("invalid"),
					},
				}

				var storeSecret v1.Secret
				Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(secret), &storeSecret)).ToNot(HaveOccurred())

				storeSecretData = storeSecret.Data

				Expect(k8sClient.Update(context.Background(), secret)).ToNot(HaveOccurred())

				Eventually(func() (string, error) {
					store := &airlockv1alpha1.MongoDBBackupStore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backupStoreName,
						Namespace: mongoNamespace,
					}, store)
					if err != nil {
						return "", err
					}
					return store.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseNotReady))
			})

			AfterAll(func() {
				Expect(k8sClient.Update(context.Background(), &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      backupStoreSecretName,
						Namespace: mongoNamespace,
					},
					Data: storeSecretData,
				})).ToNot(HaveOccurred())
			})
		}, Ordered)

		Context("MongoDBRestore", func() {
			BeforeAll(func() {
				By("Ensuring backup store is ready")
				Expect(cluster.ApplyMongodbBackupStore()).ToNot(HaveOccurred())

				Eventually(func() (string, error) {
					store := &airlockv1alpha1.MongoDBBackupStore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backupStoreName,
						Namespace: mongoNamespace,
					}, store)

					if err != nil {
						return "", err
					}

					return store.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseReady))

				By("Loading backup image")
				Expect(cluster.LoadBackupImage()).ToNot(HaveOccurred())
			})

			It("should eventually complete restore", func() {
				restore := validRestore.DeepCopy()
				err := k8sClient.Create(context.Background(), restore)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for restore to eventually complete")
				Eventually(func() (string, error) {
					restoreCR := &airlockv1alpha1.MongoDBRestore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      restore.Name,
						Namespace: restore.Namespace,
					}, restoreCR)
					if err != nil {
						return "", err
					}

					return restoreCR.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseCompleted))
			})
		}, Ordered)

		Context("MongoDBBackup", func() {
			BeforeAll(func() {
				By("Adding age secret")
				Expect(cluster.AddAgeSecret()).ToNot(HaveOccurred())

				By("Ensuring backup store is ready")
				Expect(cluster.ApplyMongodbBackupStore()).ToNot(HaveOccurred())

				Eventually(func() (string, error) {
					store := &airlockv1alpha1.MongoDBBackupStore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backupStoreName,
						Namespace: mongoNamespace,
					}, store)

					if err != nil {
						return "", err
					}

					return store.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseReady))

				// By("Loading sample data to mongo")
				// Expect(cluster.LoadSampleDataToMongo()).ToNot(HaveOccurred())

				By("Loading backup image")
				Expect(cluster.LoadBackupImage()).ToNot(HaveOccurred())
			})

			It("should create backup and eventually complete", func() {
				By("Creating MongoDBBackup resource")
				backup := validBackupSpec.DeepCopy()

				// creating the backup should immediately set the bucket store ready condition to Pending
				err := k8sClient.Create(context.Background(), backup)
				Expect(err).ToNot(HaveOccurred())

				By("initially backup should be in Pending phase")
				Eventually(func() (string, error) {
					backupCR := &airlockv1alpha1.MongoDBBackup{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backup.Name,
						Namespace: backup.Namespace,
					}, backupCR)
					if err != nil {
						return "", err
					}

					return backupCR.Status.Phase, nil
				}, 10*time.Second, 1*time.Second).Should(Or(Equal(PhasePending)))

				By("Waiting for backup to eventually complete")
				Eventually(func() (string, error) {
					backupCR := &airlockv1alpha1.MongoDBBackup{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backup.Name,
						Namespace: backup.Namespace,
					}, backupCR)
					if err != nil {
						return "", err
					}

					return backupCR.Status.Phase, nil
				}, 3*time.Minute, 10*time.Second).Should(Equal(PhaseCompleted))

				By("Verifying backup phase is Completed")
				backupCR := &airlockv1alpha1.MongoDBBackup{}
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name:      backup.Name,
					Namespace: backup.Namespace,
				}, backupCR)
				Expect(err).ToNot(HaveOccurred())
				Expect(backupCR.Status.Phase).To(Equal(PhaseCompleted))
			})

			It("should create backup file in the PVC volume", func() {
				backup := validBackupSpec.DeepCopy()

				root, err := utils.GetRootDir()
				Expect(err).ToNot(HaveOccurred())

				By("Finding the PVC created for the backup")
				var pvc v1.PersistentVolumeClaim
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name:      backup.Name,
					Namespace: backup.Namespace,
				}, &pvc)
				Expect(err).ToNot(HaveOccurred())
				Expect(pvc.Spec.VolumeName).ToNot(BeEmpty(), "PVC should be bound to a volume")

				By("Finding the PV bound to the PVC")
				var pv v1.PersistentVolume
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name: pvc.Spec.VolumeName,
				}, &pv)
				Expect(err).ToNot(HaveOccurred())

				By("Determining the volume path on the host")
				var directoryName string
				if pv.Spec.HostPath != nil {
					directoryName = filepath.Base(pv.Spec.HostPath.Path)
				} else {
					directoryName = pv.Name
				}

				relativeDiskPath := filepath.Join("tests", "k3d", "disk", directoryName)

				By(fmt.Sprintf("Checking if backup file exists at %s", relativeDiskPath))

				_, err = os.Stat(filepath.Join(root, relativeDiskPath, "backup.gz"))
				Expect(err).ToNot(HaveOccurred())

			})

			It("should set phase to Failed when backup fails", func() {
				backup := validBackupSpec.DeepCopy()

				failedBackupName := "test-backup-failed"

				backup.Name = failedBackupName

				backup.Spec.Database = "somedb"
				backup.Spec.BackupStoreRef.Name = "nonexistent-store"

				By("Creating MongoDBBackup resource with non-existent database")

				err := k8sClient.Create(context.Background(), backup)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for backup to eventually fail")
				Eventually(func() string {
					backupCR := &airlockv1alpha1.MongoDBBackup{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      failedBackupName,
						Namespace: backup.Namespace,
					}, backupCR)
					if err != nil {
						return ""
					}

					return backupCR.Status.Phase
				}, 3*time.Minute, 10*time.Second).Should(Equal(PhaseFailed))

				By("Verifying backup phase is Failed")
				backupCR := &airlockv1alpha1.MongoDBBackup{}
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Name:      failedBackupName,
					Namespace: backup.Namespace,
				}, backupCR)
				Expect(err).ToNot(HaveOccurred())
				Expect(backupCR.Status.Phase).To(Equal(PhaseFailed))
			})
		})

		Context("MongoDBBackupSchedule", func() {
			scheduleName := "test-backup-schedule"

			BeforeAll(func() {
				By("Ensuring backup store is ready")
				Expect(cluster.ApplyMongodbBackupStore()).ToNot(HaveOccurred())

				Eventually(func() (string, error) {
					store := &airlockv1alpha1.MongoDBBackupStore{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      backupStoreName,
						Namespace: mongoNamespace,
					}, store)
					if err != nil {
						return "", err
					}
					return store.Status.Phase, nil
				}, time.Minute, time.Second).Should(Equal(PhaseReady))
			})

			// tests phase running
			It("should create backup CRs", func() {
				By("Creating MongoDBBackupSchedule resource")
				schedule := validScheduleSpec.DeepCopy()

				err := k8sClient.Create(context.Background(), schedule)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for backup CRs to be created")
				Eventually(func() (int, error) {
					var backupList airlockv1alpha1.MongoDBBackupList
					err := k8sClient.List(context.Background(), &backupList,
						client.InNamespace("mongo"),
						client.MatchingLabels{"airlock.cloud.rocket.chat/scheduler": scheduleName})
					if err != nil {
						return 0, err
					}
					return len(backupList.Items), nil
				}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 1))

				By("Schedule should be in Running phase")
				Eventually(func() string {
					var scheduleCr airlockv1alpha1.MongoDBBackupSchedule
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      schedule.Name,
						Namespace: schedule.Namespace,
					}, &scheduleCr)
					if err != nil {
						return ""
					}

					return scheduleCr.Status.Phase
				}, 2*time.Minute, 10*time.Second).Should(Equal(PhaseRunning))
			})

			It("should create at least 2 backup CRs according to schedule", func() {
				By("Waiting for first backup to be created")
				var initialBackupCount int
				Eventually(func() (int, error) {
					var backupList airlockv1alpha1.MongoDBBackupList
					err := k8sClient.List(context.Background(), &backupList,
						client.InNamespace("mongo"),
						client.MatchingLabels{"airlock.cloud.rocket.chat/scheduler": scheduleName})
					if err != nil {
						return 0, err
					}
					initialBackupCount = len(backupList.Items)
					return initialBackupCount, nil
				}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 1))

				By("Waiting for second backup to be created")
				Eventually(func() (int, error) {
					var backupList airlockv1alpha1.MongoDBBackupList
					err := k8sClient.List(context.Background(), &backupList,
						client.InNamespace("mongo"),
						client.MatchingLabels{"airlock.cloud.rocket.chat/scheduler": scheduleName})
					if err != nil {
						return 0, err
					}
					return len(backupList.Items), nil
				}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 2))
			})

			It("should set phase to Failed when schedule fails", func() {
				By("Creating MongoDBBackupSchedule resource with invalid schedule")
				schedule := validScheduleSpec.DeepCopy()

				schedule.Spec.Schedule = "invalid"

				schedule.Name += "2"

				err := k8sClient.Create(context.Background(), schedule)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for schedule to eventually fail")
				Eventually(func() string {
					var scheduleCr airlockv1alpha1.MongoDBBackupSchedule
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      schedule.Name,
						Namespace: schedule.Namespace,
					}, &scheduleCr)
					if err != nil {
						return ""
					}
					return scheduleCr.Status.Phase
				}, 2*time.Minute, 10*time.Second).Should(Equal(PhaseFailed))
			})

			// test pending, suspended schedule
			It("should set phase to Pending when schedule is suspended", func() {
				By("Creating MongoDBBackupSchedule resource with suspended schedule")
				suspended := true
				schedule := validScheduleSpec.DeepCopy()
				schedule.Spec.Suspend = &suspended
				schedule.Name += "3"

				err := k8sClient.Create(context.Background(), schedule)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for schedule to eventually be suspended")
				Eventually(func() string {
					var scheduleCr airlockv1alpha1.MongoDBBackupSchedule
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      schedule.Name,
						Namespace: schedule.Namespace,
					}, &scheduleCr)
					if err != nil {
						return ""
					}
					return scheduleCr.Status.Phase
				}, 2*time.Minute, 10*time.Second).Should(Equal(PhasePending))
			})

			// pending, but store not found being the reason
			It("should set phase to Pending when store is not found", func() {
				By("Creating MongoDBBackupSchedule resource with store not found")
				schedule := validScheduleSpec.DeepCopy()

				schedule.Spec.BackupSpec.BackupStoreRef.Name = "nonexistent-store"

				schedule.Name += "4"

				err := k8sClient.Create(context.Background(), schedule)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for schedule to pending")
				Eventually(func() string {
					var scheduleCr airlockv1alpha1.MongoDBBackupSchedule
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Name:      schedule.Name,
						Namespace: schedule.Namespace,
					}, &scheduleCr)
					if err != nil {
						return ""
					}
					return scheduleCr.Status.Phase
				}, 2*time.Minute, 10*time.Second).Should(Equal(PhasePending))
			})
		})
	})
})
