package v1alpha1

import "sigs.k8s.io/controller-runtime/pkg/client"

// +kubebuilder:object:generate=false
type Object2 interface {
	client.Object

	SetPhase(phase string)
	GetPhase() string

	SetObservedGeneration(generation int64)
}
