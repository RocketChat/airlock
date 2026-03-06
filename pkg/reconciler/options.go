package reconciler

import (
	"github.com/RocketChat/airlock/pkg/webhook"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Option struct {
	Owner    client.Object
	Recorder record.EventRecorder
	Client   client.Client
	Webhook  *webhook.Manager
}

func NewOption(c client.Client, r record.EventRecorder, o client.Object, w *webhook.Manager) *Option {
	return &Option{
		Owner:    o,
		Recorder: r,
		Client:   c,
		Webhook:  w,
	}
}
