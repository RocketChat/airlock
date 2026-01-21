package reconciler

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// CreateOrUpdate creates the object if it doesn't exist and sets the owner reference, excludes the  Status field, sends a POST update request if object already exists and has a diff
func CreateOrUpdate(ctx context.Context, c client.Client, owner client.Object, object client.Object, mutateFn controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	if err := controllerutil.SetOwnerReference(owner, object, c.Scheme()); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.CreateOrUpdate(ctx, c, object, mutateFn)
}

// CreateOrPatch sends a patch request if object already exists and has a diff, includes Status field, sets owner reference
func CreateOrPatch(ctx context.Context, c client.Client, owner client.Object, object client.Object, mutateFn controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	if err := controllerutil.SetOwnerReference(owner, object, c.Scheme()); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.CreateOrPatch(ctx, c, object, mutateFn)
}
