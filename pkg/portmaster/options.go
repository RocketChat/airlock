package portmaster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RocketChat/airlock/pkg/conditions"
	"github.com/RocketChat/airlock/pkg/reconciler"
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

type workConfig struct {
	cluster           string
	database          string
	remotePrefix      string
	workingDirectory  string
	destinationBucket string
}

type reconcillerConfig struct {
	k8sClient     client.Client
	eventRecorder record.EventRecorder
	owner         client.Object
	condMgr       *conditions.ConditionsManager
}

type Options struct {
	waitTimeout time.Duration

	// cli options, mandatory
	workConfig *workConfig

	exporting      bool
	importing      bool
	targetFiles    bool
	targetDatabase bool
	mode           PortmasterMode

	bucketIgnoreTls bool

	reconcilerConfig *reconcillerConfig

	development bool
	image       string
}

type OptionProvider func(*Options)

func WithWaitTimeout(waitTimeout time.Duration) OptionProvider {
	return func(options *Options) {
		options.waitTimeout = waitTimeout
	}
}

func WithImage(image string) OptionProvider {
	return func(options *Options) {
		options.image = image
	}
}

func WithDevelopment(development bool) OptionProvider {
	return func(options *Options) {
		options.development = development
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

func NewWorkConfig(cluster, database, remotePrefix, workingDirectory, destinationBucket string) *workConfig {
	return &workConfig{
		cluster:           cluster,
		database:          database,
		remotePrefix:      remotePrefix,
		workingDirectory:  workingDirectory,
		destinationBucket: destinationBucket,
	}
}

func NewReconcilerConfig(k8sClient client.Client, eventRecorder record.EventRecorder, owner client.Object, condMgr *conditions.ConditionsManager) *reconcillerConfig {
	return &reconcillerConfig{
		k8sClient:     k8sClient,
		eventRecorder: eventRecorder,
		owner:         owner,
		condMgr:       condMgr,
	}
}

func (o *Options) MustValidate() {
	if o.workConfig == nil {
		panic("workConfig is required")
	}

	if o.workConfig.cluster == "" {
		panic("workConfig.cluster is required")
	}
	if o.workConfig.database == "" {
		panic("workConfig.database is required")
	}
	if o.workConfig.remotePrefix == "" {
		panic("workConfig.remotePrefix is required")
	}
	if o.workConfig.workingDirectory == "" {
		panic("workConfig.workingDirectory is required")
	}
	if o.workConfig.destinationBucket == "" {
		panic("workConfig.destinationBucket is required")
	}

	if o.reconcilerConfig == nil {
		panic("reconcilerConfig is required")
	}
	if o.reconcilerConfig.k8sClient == nil {
		panic("reconcilerConfig.k8sClient is required")
	}
	if o.reconcilerConfig.eventRecorder == nil {
		panic("reconcilerConfig.eventRecorder is required")
	}
	if o.reconcilerConfig.owner == nil {
		panic("reconcilerConfig.owner is required")
	}
	if o.reconcilerConfig.condMgr == nil {
		panic("reconcilerConfig.statusMgr is required")
	}

	if o.mode == "" {
		panic("mode is required")
	}
	if o.mode != PortmasterModeExportDatabase && o.mode != PortmasterModeImportDatabase && o.mode != PortmasterModeExportFiles && o.mode != PortmasterModeImportFiles {
		panic("mode must be export-database, import-database, export-files, or import-files")
	}
}

func (o *Options) ReconcileNamespace() string {
	return o.reconcilerConfig.owner.GetNamespace()
}

func (o *Options) ReconcileCommonName() string {
	return fmt.Sprintf("%s-%s", o.reconcilerConfig.owner.GetName(), o.mode)
}

func (o *Options) Cluster() string {
	return o.workConfig.cluster
}

func (o *Options) Database() string {
	return o.workConfig.database
}

func (o *Options) RemotePrefix() string {
	return o.workConfig.remotePrefix
}

func (o *Options) WorkingDirectory() string {
	return o.workConfig.workingDirectory
}

func (o *Options) DestinationBucket() string {
	return o.workConfig.destinationBucket
}

func (o *Options) ReconcilerOptions() *reconciler.Option {
	return reconciler.NewOption(o.reconcilerConfig.k8sClient, o.reconcilerConfig.eventRecorder, o.reconcilerConfig.owner)
}

func (o *Options) Client() client.Client {
	return o.reconcilerConfig.k8sClient
}

func (o *Options) ConditionsManager() *conditions.ConditionsManager {
	return o.reconcilerConfig.condMgr
}

func (o *Options) EventRecorder() record.EventRecorder {
	return o.reconcilerConfig.eventRecorder
}

type ClientReader interface {
	client.Reader
}

var _ ClientReader = (*Options)(nil)

func (o *Options) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return o.Client().Get(ctx, key, obj, opts...)
}

func (o *Options) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return o.Client().List(ctx, list, opts...)
}

func (o *Options) WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, o.waitTimeout)
}

func (o *Options) IsExporting() bool {
	return o.exporting
}

func (o *Options) IsImporting() bool {
	return o.importing
}

func (o *Options) IsTargetFiles() bool {
	return o.targetFiles
}

func (o *Options) IsTargetDatabase() bool {
	return o.targetDatabase
}

func (o *Options) IsDevelopment() bool {
	return o.development
}

func (o *Options) Image() string {
	return o.image
}

func (o *Options) IgnoreTls() bool {
	return o.bucketIgnoreTls
}

func (o *Options) Mode() PortmasterMode {
	return o.mode
}

func (o *Options) CliArgs() []string {
	args := []string{
		"--log-format=json",
	}

	if o.IsExporting() {
		args = append(args, "export", "--upload")
	}

	if o.IsImporting() {
		args = append(args, "import")
	}

	if o.IsDevelopment() {
		args = append(args, "--log-level=debug")
	} else {
		args = append(args, "--log-level=info")
	}

	if o.IsTargetFiles() {
		args = append(args, "--target-files")
	}

	if o.IsTargetDatabase() {
		args = append(args, "--target-database")
	}

	args = append(args, "--split=true", "-r", o.RemotePrefix(), o.WorkingDirectory())

	return args
}
