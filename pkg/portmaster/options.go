package portmaster

import (
	"strings"
	"time"

	"github.com/RocketChat/airlock/pkg/conditions"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type _bucketSecretKeys struct {
	bucket          string
	region          string
	accessKeyId     string
	secretAccessKey string
}

var bucketSecretKeys = _bucketSecretKeys{
	bucket:          "bucket",
	region:          "region",
	accessKeyId:     "accessKeyId",
	secretAccessKey: "secretAccessKey",
}

type Options struct {
	waitTimeout time.Duration

	// cli options, mandatory
	cluster          string
	database         string
	remotePrefix     string
	bucketSecretName string
	exporting        bool
	importing        bool
	targetFiles      bool
	targetDatabase   bool
	mode             PortmasterMode
	workingDirectory string

	bucketIgnoreTls bool

	k8sClient     client.Client
	eventRecorder record.EventRecorder
	owner         client.Object
	statusMgr     *conditions.ConditionsManager

	development bool
	image       string
}

type OptionProvider func(*Options)

func WithWaitTimeout(waitTimeout time.Duration) OptionProvider {
	return func(options *Options) {
		options.waitTimeout = waitTimeout
	}
}

func WithDevelopment(development bool) OptionProvider {
	return func(options *Options) {
		options.development = development
	}
}

func WithImage(image string) OptionProvider {
	return func(options *Options) {
		options.image = image
	}
}

func WithK8sClient(k8sClient client.Client) OptionProvider {
	return func(options *Options) {
		options.k8sClient = k8sClient
	}
}

func WithEventRecorder(eventRecorder record.EventRecorder) OptionProvider {
	return func(options *Options) {
		options.eventRecorder = eventRecorder
	}
}

func WithStatusMgr(statusMgr *conditions.ConditionsManager) OptionProvider {
	return func(options *Options) {
		options.statusMgr = statusMgr
	}
}

func WithOwner(owner client.Object) OptionProvider {
	return func(options *Options) {
		options.owner = owner
	}
}

func WithCluster(cluster string) OptionProvider {
	return func(options *Options) {
		options.cluster = cluster
	}
}

func WithDatabase(database string) OptionProvider {
	return func(options *Options) {
		options.database = database
	}
}

func WithRemotePrefix(remotePrefix string) OptionProvider {
	return func(options *Options) {
		options.remotePrefix = remotePrefix
	}
}

func WithBucketSecretName(bucketSecretName string) OptionProvider {
	return func(options *Options) {
		options.bucketSecretName = bucketSecretName
	}
}

func WithBucketIgnoreTls(bucketIgnoreTls bool) OptionProvider {
	return func(options *Options) {
		options.bucketIgnoreTls = bucketIgnoreTls
	}
}

func WithMode(mode PortmasterMode) OptionProvider {
	return func(options *Options) {
		parts := strings.Split(string(mode), "-")
		switch parts[0] {
		case "export":
			options.exporting = true
		case "import":
			options.importing = true
		}
		switch parts[1] {
		case "files":
			options.targetFiles = true
		case "database":
			options.targetDatabase = true
		}
		options.mode = mode
	}
}

func WithWorkingDirectory(workingDirectory string) OptionProvider {
	return func(options *Options) {
		options.workingDirectory = workingDirectory
	}
}
