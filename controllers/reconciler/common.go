package reconciler

import (
	"context"
	"fmt"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrReconcilerInvalidOptions = fmt.Errorf("invalid options")

func wrapInReconcilerError(err error) error {
	return fmt.Errorf("reconciler error: %w, %w", ErrReconcilerInvalidOptions, err)
}

func reason(object client.Object, result controllerutil.OperationResult, err error) string {
	kind := object.GetObjectKind().GroupVersionKind().Kind

	if err != nil {
		return fmt.Sprintf("%sReconcileFailed", kind)
	}

	switch result {
	case controllerutil.OperationResultCreated:
		return fmt.Sprintf("%sCreated", kind)
	case controllerutil.OperationResultUpdated:
		return fmt.Sprintf("%sUpdated", kind)
	default:
		return fmt.Sprintf("%sUnknown", kind)
	}
}

func recordEvent(r record.EventRecorder, owner client.Object, object client.Object, result controllerutil.OperationResult, err error) {
	if result == controllerutil.OperationResultNone {
		return
	}

	eventReason := reason(object, result, err)

	if err != nil {
		r.Eventf(owner, corev1.EventTypeWarning, eventReason, "Failed to reconcile object: %s, err: %s", object.GetName(), err.Error())
		return
	}

	r.Eventf(owner, corev1.EventTypeNormal, eventReason, "%s/%s", object.GetNamespace(), object.GetName())
}

// CreateOrUpdate creates the object if it doesn't exist and sets the owner reference, excludes the  Status field, sends a POST update request if object already exists and has a diff
func CreateOrUpdate(ctx context.Context, object client.Object, mutateFn controllerutil.MutateFn, o *Option) (controllerutil.OperationResult, error) {
	var (
		owner = o.Owner
		r     = o.Recorder
		c     = o.Client
	)

	if err := controllerutil.SetControllerReference(owner, object, c.Scheme()); err != nil {
		return controllerutil.OperationResultNone, err
	}

	result, err := controllerutil.CreateOrUpdate(ctx, c, object, mutateFn)
	if err != nil {
		recordEvent(r, owner, object, result, err)

		return controllerutil.OperationResultNone, err
	}

	recordEvent(r, owner, object, result, nil)

	return result, nil
}

func CreateOrPatch(ctx context.Context, object client.Object, mutateFn controllerutil.MutateFn, o *Option) (controllerutil.OperationResult, error) {
	var (
		owner = o.Owner
		r     = o.Recorder
		c     = o.Client
	)

	if err := controllerutil.SetControllerReference(owner, object, c.Scheme()); err != nil {
		return controllerutil.OperationResultNone, wrapInReconcilerError(err)
	}

	result, err := controllerutil.CreateOrPatch(ctx, c, object, mutateFn)
	if err != nil {
		recordEvent(r, owner, object, result, err)

		return controllerutil.OperationResultNone, err
	}

	recordEvent(r, owner, object, result, nil)

	return result, nil
}

// Create creates the resource with the owner reference
func Create(ctx context.Context, object client.Object, o *Option) error {
	var (
		owner = o.Owner
		c     = o.Client
		r     = o.Recorder
	)

	if err := controllerutil.SetControllerReference(owner, object, c.Scheme()); err != nil {
		return err
	}

	err := c.Create(ctx, object)
	if err != nil {
		recordEvent(r, owner, object, controllerutil.OperationResultNone, err)

		return err
	}

	recordEvent(r, owner, object, controllerutil.OperationResultCreated, nil)

	return nil
}

func IsOwnedBy(ctx context.Context, c client.Client, owner client.Object, object client.Object) (bool, error) {
	return controllerutil.HasOwnerReference(object.GetOwnerReferences(), owner, c.Scheme())
}

const (
	EventReasonAccessRequestNotOwned = "MongoDBAccessRequestNotOwned"
	EventReasonAccessRequestNotReady = "MongoDBAccessRequestNotReady"
)

func ReconcileAccessRequest(ctx context.Context, name, namespace, cluster, database string, waitForReady func(context.Context, client.Object) error, o *Option) (*airlockv1alpha1.MongoDBAccessRequest, error) {
	var (
		owner = o.Owner
		r     = o.Recorder
		c     = o.Client
	)

	logger := log.FromContext(ctx)

	var accessRequest airlockv1alpha1.MongoDBAccessRequest
	accessRequest.Name = name
	accessRequest.Namespace = namespace

	result, err := CreateOrPatch(ctx, &accessRequest, func() error {
		accessRequest.Spec.ClusterName = cluster
		accessRequest.Spec.Database = database
		accessRequest.Spec.UserName = name + "-user"
		accessRequest.Spec.SecretName = name + "-mongodb-access-secret"
		return nil
	}, o)

	if err != nil {
		logger.Error(err, "failed to reconcile access request")

		return nil, err
	}

	if result == controllerutil.OperationResultNone {
		ownedByUs, err := IsOwnedBy(ctx, c, owner, &accessRequest)
		if err != nil {
			return nil, err
		}

		if ownedByUs {
			// nothing changed and owned
			// no need to wait for it to be ready, caller should check
			return &accessRequest, nil
		}

		r.Eventf(owner, corev1.EventTypeWarning, EventReasonAccessRequestNotOwned, "access request %s is not owned by us, proceeding with caution", accessRequest.Name)
	}

	if waitForReady != nil {
		if err := waitForReady(ctx, &accessRequest); err != nil {
			err := fmt.Errorf("failed to wait for access request to be ready: %w", err)

			logger.Error(err, "failed to wait for access request to be ready")

			r.Event(owner, corev1.EventTypeWarning, EventReasonAccessRequestNotReady, err.Error())

			return nil, err
		}
	}

	return &accessRequest, nil
}

const (
	EventReasonPersistentVolumeClaimResizeFailed = "PersistentVolumeClaimResizeFailed"
	EventReasonPersistentVolumeNotFound          = "PersistentVolumeNotFound"
	EventReasonPersistentVolumeClaimTooSmall     = "PersistentVolumeClaimTooSmall"
	EventReasonPersistentVolumeClaimNotOwned     = "PersistentVolumeClaimNotOwned"
)

func ReconcilePersistentVolumeClaim(ctx context.Context, name, namespace string, size int64, o *Option) (*v1.PersistentVolumeClaim, error) {
	var (
		owner = o.Owner
		r     = o.Recorder
		c     = o.Client
	)

	logger := log.FromContext(ctx)

	pvc := &v1.PersistentVolumeClaim{}

	pvc.Name = name
	pvc.Namespace = namespace

	err := c.Get(ctx, client.ObjectKeyFromObject(pvc), pvc)
	if client.IgnoreNotFound(err) != nil {
		err2 := fmt.Errorf("failed to get pvc %s: %w", pvc.Name, err)

		r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeNotFound, err2.Error())

		return nil, err2
	}

	// min 1g
	requestSize := max(size, 1024*1024*1024)

	requestQuantity := resource.NewQuantity(requestSize, resource.BinarySI)

	pvcSpec := v1.PersistentVolumeClaimSpec{
		AccessModes: []v1.PersistentVolumeAccessMode{
			v1.ReadWriteOnce,
		},
		Resources: v1.VolumeResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceStorage: *requestQuantity,
			},
		},
	}

	exisingStorage := pvc.Spec.Resources.Requests.Storage()

	exists := err == nil

	if !exists {
		logger.Info("pvc does not exist, creating", "name", pvc.Name, "namespace", pvc.Namespace, "size", requestQuantity.String())
		// just create it
		pvc.Spec = pvcSpec

		err := Create(ctx, pvc, o)
		if err != nil {
			return nil, err
		}

		return pvc, nil
	}

	ownedByUs, err := IsOwnedBy(ctx, c, owner, pvc)
	if err != nil {
		return nil, fmt.Errorf("pvc reconciliationn failed: failed to check if pvc %s is owned by us: %w", pvc.Name, err)
	}

	if exisingStorage.CmpInt64(size) == -1 {
		err := fmt.Errorf("pvc %s has a smaller size than the required size, existing: %s, required: %s", pvc.Name, exisingStorage.String(), requestQuantity.String())

		r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimTooSmall, err.Error())

		logger.Error(err, "pvc size is too small, checking if can be resized")

		if pvc.Spec.StorageClassName == nil {
			err := fmt.Errorf("pvc %s has no storage class, cannot resize, existing: %s, required: %s", pvc.Name, exisingStorage.String(), requestQuantity.String())
			r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimTooSmall, err.Error())
			return nil, err
		}

		if !ownedByUs {
			err := fmt.Errorf("cannot resize pvc %s, it is not owned by us, existing: %s, required: %s", pvc.Name, exisingStorage.String(), requestQuantity.String())

			r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimResizeFailed, err.Error())

			return nil, err
		}

		// we only attempt to resize if the pvc is owned by us

		// https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims

		storageClass := &storagev1.StorageClass{}

		storageClass.SetName(*pvc.Spec.StorageClassName)

		err2 := c.Get(ctx, client.ObjectKeyFromObject(storageClass), storageClass)
		if err2 != nil {
			err2 := fmt.Errorf("failed to get storage class %s: %w, unable to resize pvc: %w", *pvc.Spec.StorageClassName, err2, err)
			r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimResizeFailed, err2.Error())
			return nil, err2
		}

		if storageClass.AllowVolumeExpansion != nil || !*storageClass.AllowVolumeExpansion {
			err2 := fmt.Errorf("storage class %s does not allow volume expansion, unable to resize pvc, unable to resize pvc: %w", *pvc.Spec.StorageClassName, err)
			r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimResizeFailed, err2.Error())
			return nil, err2
		}
	} else {
		if ownedByUs {
			return pvc, nil
		}
		// size is sufficient
		msg := fmt.Sprintf("pvc %s is not owned by us but has a sufficient size, attempting to reuse, existing: %s, required: %s", pvc.Name, exisingStorage.String(), requestQuantity.String())

		logger.Info(msg)

		r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimNotOwned, msg)

		return pvc, nil
	}

	// conditions met:
	// size is insufficiet AND
	// controller owns the pvc AND
	// storage class allows volume expansion

	base := pvc.DeepCopy()

	pvc.Spec.Resources.Requests.Storage().Set(requestSize)

	if err := c.Patch(ctx, pvc, client.MergeFrom(base)); err != nil {
		logger.Error(err, "failed to patch pvc")

		r.Eventf(owner, corev1.EventTypeWarning, EventReasonPersistentVolumeClaimResizeFailed, err.Error())

		return nil, fmt.Errorf("failed to patch pvc: %w", err)
	}

	return pvc, nil
}
