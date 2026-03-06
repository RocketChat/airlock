package utils

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func UpdateObjectGVK(object client.Object, scheme *runtime.Scheme) error {
	gvk := object.GetObjectKind().GroupVersionKind()
	if !gvk.Empty() {
		return nil
	}

	gvk, err := apiutil.GVKForObject(object, scheme)
	if err != nil {
		return err
	}

	object.GetObjectKind().SetGroupVersionKind(gvk)

	return nil
}
