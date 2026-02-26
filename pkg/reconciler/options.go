package reconciler

import (
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Option struct {
	Owner    client.Object
	Recorder record.EventRecorder
	Client   client.Client
}

func NewOption(c client.Client, r record.EventRecorder, o client.Object) *Option {
	return &Option{
		Owner:    o,
		Recorder: r,
		Client:   c,
	}
}
