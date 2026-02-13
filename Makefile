# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.1.0

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# rocket.chat/airlock-bundle:$VERSION and rocket.chat/airlock-catalog:$VERSION.
IMAGE_TAG_BASE ?= rocketchat/airlock

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)
	
BIMG ?= backup:latest

# Reusable kubectl command with kubeconfig
# KUBECTL_WITH_CONFIG = k3d kubeconfig print ${NAME} > /tmp/${NAME}.kube.config && KUBECONFIG=/tmp/${NAME}.kube.config kubectl
KUBECTL_WITH_CONFIG = KUBECONFIG=/tmp/${NAME}.kube.config kubectl

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec


# Target OS and architecture
TARGETOS ?= linux
TARGETARCH ?= amd64

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet ## Run tests (Ginkgo suite only).
	go test ./tests/ -v -ginkgo.v -coverprofile cover.out

.PHONY: test-unit
test-unit: ## Run unit tests (excludes Ginkgo suite).
	go test -tags=unit ./internal/... -v -count=1

##@ Build

.PHONY: build
build: generate manifests fmt vet ## Build manager binary.
	CGO_ENABLED=0 GOOS=$(TARGETOS) GOARCH=$(TARGETARCH) go build -o bin/manager main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-build-no-test
docker-build-no-test: build
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> than the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross
	- docker buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.19.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: bundle
bundle: manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle $(BUNDLE_GEN_FLAGS)
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: k3d-cluster
k3d-cluster:
ifndef NAME
	$(error NAME is required. Usage: make k3d-cluster NAME=my-cluster)
endif
	test -d tests/k3d/disk || mkdir -pv tests/k3d/disk
	k3d cluster list -o json | jq '.[].name' -r | grep -q ${NAME} || \
		k3d cluster create ${NAME} --kubeconfig-update-default=false --kubeconfig-switch-context=false --no-lb --no-rollback --wait -s1 -a1 --volume $(PWD)/tests/k3d/disk:/disk --k3s-arg "--disable=local-storage@server:*"
	k3d kubeconfig print ${NAME} > /tmp/${NAME}.kube.config
	
.PHONY: k3d-add-storageclass
k3d-add-storageclass: k3d-cluster
	$(KUBECTL_WITH_CONFIG) apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.34/deploy/local-path-storage.yaml
	$(KUBECTL_WITH_CONFIG) apply -f tests/assets/k3d/local-path-config.yaml
	$(KUBECTL_WITH_CONFIG) rollout restart deployment/local-path-provisioner -n local-path-storage
	$(KUBECTL_WITH_CONFIG) rollout status deployment/local-path-provisioner -n local-path-storage
	$(KUBECTL_WITH_CONFIG) annotate storageclass local-path storageclass.kubernetes.io/is-default-class- || true
	$(KUBECTL_WITH_CONFIG) apply -f tests/assets/k3d/manual-storageclass.yaml
	
.PHONY: k3d-load-image
k3d-load-image: docker-build-no-test k3d-cluster k3d-add-storageclass
	k3d image load ${IMG} -c ${NAME}
	
.PHONY: k3d-deploy
k3d-deploy-airlock: k3d-load-image
	$(KUBECTL_WITH_CONFIG) apply -f config/crd/bases
	$(KUBECTL_WITH_CONFIG) get namespace airlock-system 2>&1 >/dev/null || $(KUBECTL_WITH_CONFIG) create namespace airlock-system
	$(KUBECTL_WITH_CONFIG) apply -k config/rbac
	$(KUBECTL_WITH_CONFIG) apply -f config/manager/manager.yaml
	$(KUBECTL_WITH_CONFIG) apply -f tests/assets/airlock
	$(KUBECTL_WITH_CONFIG) set env deployment/controller-manager DEV_MODE=true -n airlock-system
	
.PHONY: k3d-destroy
k3d-destroy:
ifndef NAME
	$(error NAME is required. Usage: make k3d-cluster NAME=my-cluster)
endif
	k3d cluster delete ${NAME}

.PHONY: k3d-deploy-mongo
k3d-deploy-mongo: k3d-cluster
	$(KUBECTL_WITH_CONFIG) apply -f ./tests/assets/mongo

.PHONY: k3d-deploy-minio
k3d-deploy-minio: k3d-cluster k3d-add-storageclass
	$(KUBECTL_WITH_CONFIG) apply -k "github.com/minio/operator?ref=v6.0.4" 
	$(KUBECTL_WITH_CONFIG) rollout status deployment/minio-operator -n minio-operator
	$(KUBECTL_WITH_CONFIG) apply -f ./tests/assets/minio
	
.PHONY: docker-build-backup-image
docker-build-backup-image:
	docker build -t ${BIMG} backup-image/
	
.PHONY: k3d-load-backup-image
k3d-load-backup-image: k3d-cluster docker-build-backup-image
	k3d image import -c ${NAME} ${BIMG}
	
.PHONY: k3d-run-backup-pod
k3d-run-backup-pod: k3d-cluster k3d-load-backup-image
	$(KUBECTL_WITH_CONFIG) apply -f ./tests/assets/local-tests/backup-pod.yaml
	
.PHONY: k3d-load-mongo-data
k3d-load-mongo-data: k3d-deploy-mongo
	$(KUBECTL_WITH_CONFIG) apply -f ./tests/assets/local-tests/mongo-restore-job.yaml
	
# subject to change as more matures
.PHONY: k3d-setup-all
k3d-setup-all: k3d-load-mongo-data k3d-load-backup-image k3d-deploy-airlock k3d-deploy-minio

.PHONY: k3d-retsart-airlock
k3d-restart-airlock:
ifndef NAME
	$(error NAME is required. Usage: make k3d-restart-airlock NAME=my-cluster)
endif
	$(KUBECTL_WITH_CONFIG) rollout restart deployment controller-manager -n airlock-system
	
# Example: make k3d-kubectl NAME=airlock-test get pods \\-A
k3d-kubectl:
ifndef NAME
	$(error NAME is required. Usage: make k3d-kubectl NAME=my-cluster [kubectl args...])
endif
	$(KUBECTL_WITH_CONFIG) $(wordlist 2, $(words $(MAKECMDGOALS)), $(MAKECMDGOALS))

# Support for passing commands after the target name
%::
	@:

.PHONY: k3d-add-backup-store
k3d-add-backup-store: k3d-cluster
	$(KUBECTL_WITH_CONFIG) apply -f ./tests/assets/local-tests/mongodbbucketstoresecret.yaml
	$(KUBECTL_WITH_CONFIG) apply -f ./config/samples/airlock_v1alpha1_mongodbbackupstore.yaml