/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/RocketChat/airlock/controllers/hash"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

// MongoDBAccessRequestReconciler reconciles a MongoDBAccessRequest object
type MongoDBAccessRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MongoDBAccessRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *MongoDBAccessRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Started MongoDBAccessRequestReconciler reconcile")

	mongodbAccessRequestCR := &airlockv1alpha1.MongoDBAccessRequest{}

	err := r.Get(ctx, req.NamespacedName, mongodbAccessRequestCR)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Operator resource object not found.")

		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Error getting operator resource object")

		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Degraded",
				Status:             metav1.ConditionTrue,
				Reason:             ReasonCRNotAvailable,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("unable to get operator custom resource: %s", err.Error()),
			})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
		metav1.Condition{
			Type:               "Initializing",
			Status:             metav1.ConditionTrue,
			Reason:             "ReasonCRInitializing",
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            fmt.Sprintf("Initializing access request for: %s", mongodbAccessRequestCR.Name),
		})

	mongodbClusterCR := &airlockv1alpha1.MongoDBCluster{}

	// Get mongo cluster from given name
	mongodbClusterCR.Name = req.Name

	// TODO: username and password validation?

	// TODO: Get namespace from the operator
	r.Get(ctx, types.NamespacedName{Namespace: "airlock-system", Name: mongodbAccessRequestCR.Spec.ClusterName}, mongodbClusterCR)

	// TODO: generate database if empty and store it on CR

	// TODO: generate password if empty and store on CR

	// TODO: generate secretname if empty and store on CR

	// Connect to mongo and create user with that password
	//client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))

	// TODO: create secret with connection string

	return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
}

// SetupWithManager sets up the controller with the Manager.
func (r *MongoDBAccessRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBAccessRequest{}).
		Complete(r)
}

func (r *MongoDBAccessRequestReconciler) reconcileSecret(ctx context.Context, req ctrl.Request, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, mongodbClusterCR *airlockv1alpha1.MongoDBCluster) error {
	logger := log.FromContext(ctx)

	connectionSecret := &corev1.Secret{}
	create := false

	// TODO: does this need sanitization?
	connectionString := fmt.Sprintf("mongodb://%s:%s@%s:%s/%s%s",
		mongodbAccessRequestCR.Spec.UserName,
		mongodbAccessRequestCR.Spec.Password,
		mongodbClusterCR.Spec.HostTemplate,
		mongodbClusterCR.Spec.PortTemplate,
		mongodbAccessRequestCR.Spec.Database,
		mongodbClusterCR.Spec.OptionsTemplate)

	logger.Info("Reconciling secret for " + mongodbAccessRequestCR.Name + " using cluster " + mongodbClusterCR.Name)

	err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: mongodbAccessRequestCR.Spec.SecretName}, connectionSecret)

	if err != nil && errors.IsNotFound(err) {
		create = true

		connectionSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mongodbAccessRequestCR.Spec.SecretName,
				Namespace: req.Namespace,
			},
			Data: map[string][]byte{
				"connectionString": []byte(base64.StdEncoding.EncodeToString([]byte(connectionString))),
			},
		}
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("Error getting existing RocketChat ingress: %q", req.NamespacedName))

		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Error",
				Status:             metav1.ConditionTrue,
				Reason:             "UnableToGetSecret",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("unable to get secret %q: %s", req.NamespacedName, err.Error()),
			})

		return utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	connectionSecret.Data["connectionString"] = []byte(base64.StdEncoding.EncodeToString([]byte(connectionString)))

	_ = ctrl.SetControllerReference(mongodbAccessRequestCR, connectionSecret, r.Scheme)
	expectedHash := hash.Object(connectionSecret.Data)
	actualHash := connectionSecret.Annotations["ResourceHash"] // zero value ok
	if create {
		logger.Info(fmt.Sprintf("creating RocketChat ingress %q: expected hash: %q", req.NamespacedName, expectedHash))

		err = r.Create(ctx, connectionSecret)
	} else if expectedHash != actualHash {
		logger.Info(fmt.Sprintf("updating RocketChat ingress %q: expected hash %q does not match actual hash %q", req.NamespacedName, expectedHash, actualHash))

		err = r.Update(ctx, connectionSecret)
	}
	if err != nil {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Error",
				Status:             metav1.ConditionTrue,
				Reason:             ReasonOperandIngressFailed,
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("unable to update secret %q: %s", req.NamespacedName, err.Error()),
			})

		return utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}
	return nil
}

func (r *MongoDBAccessRequestReconciler) createMongoDBUser(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest) error {
	logger := log.FromContext(ctx)

	logger.Info("Creating MongoDB user")

	//TODO: everything

	return nil
}
