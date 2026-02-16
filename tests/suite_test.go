/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tests

import (
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/RocketChat/airlock/tests/utils"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient client.Client
	kubectl   *utils.Kubectl
	cluster   utils.K3dCluster
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error

	By("bootstrapping test environment")
	cluster = utils.NewK3dCluster("airlock-test")

	err = cluster.Start()
	Expect(err).NotTo(HaveOccurred())

	By("get kubectl handler")
	kubectl, err = cluster.Kubectl()
	Expect(err).NotTo(HaveOccurred())
	Expect(kubectl).NotTo(BeNil())

	By("get k8s client")
	k, err := cluster.K8sClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(k).NotTo(BeNil())
	k8sClient = *k

	kubectl.SetK8sClient(k8sClient)

	time.Sleep(30 * time.Second)

	By("Deploy mongodb")
	Expect(cluster.DeployMongo()).NotTo(HaveOccurred())

	By("Deploy minio")
	Expect(cluster.DeployMinio()).NotTo(HaveOccurred())

	By("Deploy airlock")
	Expect(cluster.DeployAirlock()).NotTo(HaveOccurred())

	By("apply CRDs")
	err = kubectl.Apply(filepath.Join("..", "config", "crd", "bases"))
	Expect(err).NotTo(HaveOccurred())

	By("load mongodb sample data for testing")
	Expect(utils.RunStreamOutput("make", "k3d-load-mongo-data", utils.MakeVar("NAME", "airlock-test")))
})

var _ = ReportAfterSuite("Teardown cluster", func(report types.Report) {
	// Check if any spec in the suite failed
	failedCount := report.SpecReports.CountWithState(types.SpecStateFailed)

	if failedCount > 0 {
		By("Skipping teardown of test cluster since one or more specs failed")
		By("Use 'make k3d-kubectl NAME=airlock-test' to debug the cluster")
		return
	}

	By("tearing down the test environment")
	err := cluster.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = cluster.Delete()
	Expect(err).NotTo(HaveOccurred())
})
