# Airlock
Airlock is an kubernetes operator that manages access and secrets for MongoDB clusters.

## Description
Inspired by [cert-manager](https://github.com/cert-manager/cert-manager), Airlock receives requests through CustomResources to create credentials for databases on MongoDB and returns the connection string as secrets to be consumed by other  applications.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:
	
```sh
make docker-build docker-push IMG=<some-registry>/airlock:tag
```
	
3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/airlock:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller to the cluster:

```sh
make undeploy
```


### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources untile the desired state is reached on the cluster 

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Testing

A testing environment can be spun up locally with k3d. 

### Quick Start

Run
```sh
make k3d-load-mongo-data k3d-deploy-airlock k3d-deploy-minio NAME=airlock IMG=controller:latest
```

This
1. deploys a k3d cluster
2. sets up storage class that uses local path `tests/k3d/disk`
3. deploys minio
4. deploys mongo
5. loads sample data into mongo
6. deploys airlock operator

**NOTE: All tests run through `make test` use Makefile targets, i.e. should be also run manually in case needed. This is intentional to allow for debugging/troubleshooting the tests individually.**

### Makefile Targets

#### K3d Cluster Management

**`k3d-cluster`** - Create a k3d cluster
```sh
# Create a cluster named 'airlock-test'
make k3d-cluster NAME=airlock-test

# The cluster will be created with:
# - 1 server node and 1 agent node
# - Local storage mounted at tests/k3d/disk:/disk
# - Kubeconfig saved to /tmp/${NAME}.kube.config
```

**`k3d-kubectl`** - Run kubectl commands against the cluster `k3d-cluster` created.

Pass the name of the cluster as the first argument.
```sh
# Get all pods in all namespaces
make k3d-kubectl NAME=airlock-test get pods \\-A
```

Make sure to escape the backslashes in the command, i.e. `\\-` is correct, `\-` is incorrect.

**`k3d-destroy`** - Delete a k3d cluster
```sh
# Delete the cluster
make k3d-destroy NAME=airlock-test
```

When running `make test` if any of the tests fail, the cluster should not be deleted. At that point use `make k3d-kubectl NAME=airlock-test` to start debugging/troubleshooting the test.

**`k3d-add-storageclass`** - Configure storage classes for the cluster
```sh
# Sets up local-path provisioner and manual storage class
make k3d-add-storageclass NAME=airlock-test
```

#### K3d Deployment Targets

**`k3d-deploy-mongo`** - Deploy MongoDB to the cluster
```sh
# Deploy MongoDB with sample configuration
make k3d-deploy-mongo NAME=airlock-test
```

**`k3d-deploy-minio`** - Deploy MinIO to the cluster
```sh
# Deploy MinIO operator and tenant
make k3d-deploy-minio NAME=airlock-test
```

**`k3d-deploy-airlock`** - Deploy Airlock operator to the cluster
```sh
# Build, load, and deploy the operator
make k3d-deploy-airlock NAME=airlock-test IMG=controller:latest

# This target:
# - Builds the Docker image (without running tests)
# - Loads the image into the k3d cluster
# - Applies CRDs, RBAC, and manager deployment
# - Sets DEV_MODE=true for development
```

**`k3d-setup-all`** - Complete setup (mongo data, backup image, airlock, minio)
```sh
# One command to set up everything
make k3d-setup-all NAME=airlock-test IMG=controller:latest
```

#### K3d Image Management

**`k3d-load-image`** - Build and load controller image into cluster
```sh
# Build and load the controller image
make k3d-load-image NAME=airlock-test IMG=controller:latest
```

**`docker-build-backup-image`** - Build the backup image
```sh
# Build backup image (default: backup:latest)
make docker-build-backup-image

# Build with custom tag
make docker-build-backup-image BIMG=my-backup:1.0.0
```

**`k3d-load-backup-image`** - Build and load backup image into cluster
```sh
# Build and load backup image
make k3d-load-backup-image NAME=airlock-test
```

#### K3d Data Management

**`k3d-load-mongo-data`** - Load sample data into MongoDB
```sh
# Deploy a job that restores sample data to MongoDB
make k3d-load-mongo-data NAME=airlock-test
```

**`k3d-add-backup-store`** - Add backup store configuration
```sh
# Apply backup store secret and CR
make k3d-add-backup-store NAME=airlock-test
```

#### K3d Utility Targets

**`k3d-kubectl`** - Run kubectl commands against the cluster
```sh
# Get all pods in all namespaces
make k3d-kubectl NAME=airlock-test get pods \\-A

# Get MongoDBBackup resources
make k3d-kubectl NAME=airlock-test get mongodbbackups \\-A

# Describe a resource
make k3d-kubectl NAME=airlock-test describe pod my-pod \\-n mongo

# Apply a manifest
make k3d-kubectl NAME=airlock-test apply \\-f my-manifest.yaml

# Get logs
make k3d-kubectl NAME=airlock-test logs deployment/controller-manager \\-n airlock-system
```

**`k3d-restart-airlock`** - Restart the Airlock controller deployment
```sh
# Restart the controller to pick up changes
make k3d-restart-airlock NAME=airlock-test
```

**`k3d-run-backup-pod`** - Run a test backup pod
```sh
# Deploy a test pod for manual backup testing
make k3d-run-backup-pod NAME=airlock-test
```

#### Development Targets

**`build`** - Build the manager binary
```sh
# Build for current platform
make build

# Build for specific OS/arch
make build TARGETOS=linux TARGETARCH=amd64
```

**`run`** - Run the controller locally
```sh
# Run controller from your host (uses current kubeconfig)
make run
```

**`test`** - Run tests
```sh
# Run all tests with coverage
make test
```

**`manifests`** - Generate CRD and RBAC manifests
```sh
# Regenerate CRDs and RBAC after API changes
make manifests
```

**`generate`** - Generate DeepCopy code
```sh
# Generate DeepCopy methods for API types
make generate
```

#### Docker Build Targets

**`docker-build`** - Build Docker image (runs tests first)
```sh
# Build image with default tag
make docker-build

# Build with custom tag
make docker-build IMG=myregistry/airlock:v1.0.0
```

**`docker-build-no-test`** - Build Docker image without running tests
```sh
# Faster build for development
make docker-build-no-test IMG=controller:latest
```

**`docker-push`** - Push Docker image
```sh
# Push to registry
make docker-push IMG=myregistry/airlock:v1.0.0
```

#### Deployment Targets

**`install`** - Install CRDs to cluster
```sh
# Install CRDs to current kubeconfig cluster
make install
```

**`uninstall`** - Uninstall CRDs
```sh
# Remove CRDs (ignores not found errors)
make uninstall ignore-not-found=true
```

**`deploy`** - Deploy controller to cluster
```sh
# Deploy with custom image
make deploy IMG=myregistry/airlock:v1.0.0
```

**`undeploy`** - Remove controller from cluster
```sh
# Remove controller deployment
make undeploy ignore-not-found=true
```
### Common Workflows

**Complete local development setup:**
```sh
# 1. Create cluster and deploy everything
make k3d-setup-all NAME=airlock-test IMG=controller:latest

# 2. Make code changes, rebuild and restart
make docker-build-no-test IMG=controller:latest
make k3d-load-image NAME=airlock-test IMG=controller:latest
make k3d-restart-airlock NAME=airlock-test

# 3. Check logs
make k3d-kubectl NAME=airlock-test logs \\-f deployment/controller-manager \\-n airlock-system
```

**Testing backup functionality:**
```sh
# 1. Setup environment
make k3d-setup-all NAME=airlock-test IMG=controller:latest

# 2. Add backup store
make k3d-add-backup-store NAME=airlock-test

# 3. Create a backup (via kubectl or YAML)
make k3d-kubectl NAME=airlock-test apply \\-f tests/assets/local-tests/mongodbbackup.yaml

# 4. Check backup status
make k3d-kubectl NAME=airlock-test get mongodbbackups \\-A
```

**Cleanup:**
```sh
# Delete the entire cluster
make k3d-destroy NAME=airlock-test
```