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
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

// MongoDBClusterReconciler reconciles a MongoDBCluster object
type MongoDBClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MongoDBCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *MongoDBClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Started MongoDBClusterReconciler reconcile")

	mongodbClusterCR := &airlockv1alpha1.MongoDBCluster{}

	// TODO: namespace? clusters should be cluster-wide.
	err := r.Get(ctx, req.NamespacedName, mongodbClusterCR)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Operator resource object not found.")

		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Error getting operator resource object")

		meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "OperatorResourceNotAvailable",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("unable to get operator custom resource: %s", err.Error()),
			})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
	}

	//Test connection and user permissions
	err = testConnection(ctx, mongodbClusterCR)
	if err != nil {
		meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "MongoConnectionFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("mongo connection failed: %s", err.Error()),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
	}

	meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
		metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterInitialized",
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            "Cluster is ready",
		})
	return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
}

// SetupWithManager sets up the controller with the Manager.
func (r *MongoDBClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctrl.Log.WithName("controllers").WithName("MongoDBCluster").V(1).Info("Starting MongoDBCluster controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBCluster{}).
		Complete(r)
}

func testConnection(ctx context.Context, mongodbClusterCR *airlockv1alpha1.MongoDBCluster) error {
	logger := log.FromContext(ctx)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongodbClusterCR.Spec.ConnectionString))
	if err != nil {
		logger.Error(err, "Couldn't connect to mongodb for cluster "+mongodbClusterCR.Name)
		return err
	}

	// Retrieve the current user's roles and privileges
	var result map[string]interface{}
	err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "connectionStatus", Value: 1}}).Decode(&result)
	if err != nil {
		logger.Error(err, "Couldn't retrieve user info for cluster "+mongodbClusterCR.Name)
		return err
	}

	// Check if the current user has the necessary privilege in any of their roles
	hasPrivilege := false
	roles := result["authInfo"].(map[string]interface{})["authenticatedUserRoles"].(primitive.A)
	for _, role := range roles {
		if role.(map[string]interface{})["privileges"] != nil {
			for _, privilege := range role.(map[string]interface{})["privileges"].([]interface{}) {
				if privilege.(map[string]interface{})["actions"].(map[string]interface{})["userAdmin"].(bool) {
					hasPrivilege = true
					break
				}
			}
		}
		if role.(map[string]interface{})["role"] == "userAdminAnyDatabase" {
			hasPrivilege = true
			break
		}
	}
	if !hasPrivilege {
		err = errors.NewUnauthorized("User can't create new users")
		logger.Error(err, "User doesn't have privilege in cluster "+mongodbClusterCR.Name)

		return err
	}
	return nil

}
