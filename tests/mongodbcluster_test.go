package tests

import (
	//nolint:golint
	//nolint:revive

	"github.com/RocketChat/airlock/tests/utils"
	. "github.com/onsi/ginkgo/v2"

	//nolint:golint
	//nolint:revive
	. "github.com/onsi/gomega"
)

const namespace = "airlock-system"

var _ = Describe("airlock", Ordered, func() {
	BeforeAll(func() {
		By("Creating the namespace")
		err := utils.Run("kubectl", "create", "namespace", namespace)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("Deleting the namespace")
		err := utils.Run("kubectl", "delete", "namespace", namespace)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	})
})
