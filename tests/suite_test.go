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

	. "github.com/onsi/ginkgo/v2"
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

	By("load controller image")
	err = cluster.LoadImage("controller:latest")
	Expect(err).NotTo(HaveOccurred())

	By("get kubectl handler")
	kubectl, err = cluster.Kubectl()
	Expect(err).NotTo(HaveOccurred())
	Expect(kubectl).NotTo(BeNil())

	By("apply CRDs")
	err = kubectl.Apply(filepath.Join("..", "config", "crd", "bases"))
	Expect(err).NotTo(HaveOccurred())

	By("get k8s client")
	k, err := cluster.K8sClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(k).NotTo(BeNil())
	k8sClient = *k

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := cluster.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = cluster.Delete()
	Expect(err).NotTo(HaveOccurred())
})
