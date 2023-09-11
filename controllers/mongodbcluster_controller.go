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
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"go.mongodb.org/atlas/mongodbatlas"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

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
//+kubebuilder:rbac:groups="";apps;networking.k8s.io,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *MongoDBClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Started MongoDBClusterReconciler reconcile")

	mongodbClusterCR := &airlockv1alpha1.MongoDBCluster{}

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

	secret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Namespace: mongodbClusterCR.Spec.ConnectionSecretNamespace, Name: mongodbClusterCR.Spec.ConnectionSecret}, secret)
	if err != nil {
		meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "SecretReadFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Failed to read connection secret %s/%s: %s", mongodbClusterCR.Spec.ConnectionSecretNamespace, mongodbClusterCR.Spec.ConnectionSecret, err.Error()),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
	}

	_ = ctrl.SetControllerReference(mongodbClusterCR, secret, r.Scheme)

	// Test connection and user permissions
	if mongodbClusterCR.Spec.UseAtlasApi {
		err = testAtlasConnection(ctx, mongodbClusterCR, secret)
		if err != nil {
			meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
				metav1.Condition{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "AtlasConnectionFailed",
					LastTransitionTime: metav1.NewTime(time.Now()),
					Message:            fmt.Sprintf("Atlas connection failed: %s", err.Error()),
				})

			return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
		}

		// Add nodes to Atlas firewall
		if mongodbClusterCR.Spec.AtlasNodeIPAccessStrategy == "rancher-annotation" {
			err = r.reconcileAtlasFirewall(ctx, mongodbClusterCR, secret)
			if err != nil {
				meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
					metav1.Condition{
						Type:               "Ready",
						Status:             metav1.ConditionFalse,
						Reason:             "AtlasFirewallFailed",
						LastTransitionTime: metav1.NewTime(time.Now()),
						Message:            fmt.Sprintf("Failed to add nodes to atlas firewall: %s", err.Error()),
					})

				return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
			}
		}
	} else {
		err = testMongoConnection(ctx, mongodbClusterCR, secret)
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
	}

	meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
		metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterInitialized",
			LastTransitionTime: metav1.NewTime(time.Now()),
			Message:            "Cluster is ready",
		})
	r.updateAccessRequests(ctx, req, mongodbClusterCR.Name)
	return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
}

func (r *MongoDBClusterReconciler) findObjectsForSecret(secret client.Object) []reconcile.Request {
	mongodbClusterCR := &airlockv1alpha1.MongoDBClusterList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("connectionSecret", secret.GetName()),
		Namespace:     "",
	}

	err := r.List(context.TODO(), mongodbClusterCR, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(mongodbClusterCR.Items))
	for i, item := range mongodbClusterCR.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *MongoDBClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctrl.Log.WithName("controllers").WithName("MongoDBCluster").V(1).Info("Starting MongoDBCluster controller")

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &airlockv1alpha1.MongoDBCluster{}, "connectionSecret", func(rawObj client.Object) []string {
		// Extract the ConfigMap name from the ConfigDeployment Spec, if one is provided
		mongodbClusterCR := rawObj.(*airlockv1alpha1.MongoDBCluster)
		if mongodbClusterCR.Spec.ConnectionSecret == "" {
			return nil
		}

		return []string{mongodbClusterCR.Spec.ConnectionSecret}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBCluster{}).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&source.Kind{Type: &corev1.Node{}},
			handler.EnqueueRequestsFromMapFunc(func(node client.Object) []reconcile.Request {
				mongodbClusterCR := &airlockv1alpha1.MongoDBClusterList{}
				listOps := &client.ListOptions{
					Namespace: "",
				}

				err := r.List(context.TODO(), mongodbClusterCR, listOps)
				if err != nil {
					return []reconcile.Request{}
				}

				requests := make([]reconcile.Request, 0)
				for _, item := range mongodbClusterCR.Items {
					if item.Spec.AtlasNodeIPAccessStrategy != "" {
						requests = append(requests, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      item.GetName(),
								Namespace: item.GetNamespace(),
							},
						})
					}
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func testMongoConnection(ctx context.Context, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	connectionString, err := getSecretProperty(secret, "connectionString")
	if err != nil {
		return err
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
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

	hasPrivilege := canCreateUsers(logger, result["authInfo"].(map[string]interface{})["authenticatedUserRoles"].(primitive.A))

	if !hasPrivilege {
		err = errors.NewUnauthorized("User can't create new users")
		logger.Error(err, "User doesn't have privilege in cluster "+mongodbClusterCR.Name)

		return err
	}

	return nil
}

func testAtlasConnection(ctx context.Context, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	client, atlasGroupID, err := getAtlasClientFromSecret(secret)
	if err != nil {
		logger.Error(err, "Couldn't get a client for Atlas")
		return err
	}

	root, _, err := client.Root.List(context.Background(), nil)
	if err != nil {
		logger.Error(err, "Couldn't get atlas user information")
		return err
	}

	hasPrivilege := false

	for _, role := range root.APIKey.Roles {
		if role.GroupID == atlasGroupID && (role.RoleName == "GROUP_OWNER" || role.RoleName == "ORG_OWNER") {
			hasPrivilege = true
		}
	}

	if !hasPrivilege {
		err = errors.NewUnauthorized("User is not GROUP_OWNER nor ORG_OWNER")
		logger.Error(err, "User doesn't have GROUP_OWNER privilege for atlas groupID "+atlasGroupID)

		return err
	}

	return nil
}

func (r *MongoDBClusterReconciler) updateAccessRequests(ctx context.Context, req ctrl.Request, clusterName string) error {
	logger := log.FromContext(ctx)

	logger.Info("Cluster " + clusterName + " updated. Setting out of date status on AccessRequests")

	accessRequestList := &airlockv1alpha1.MongoDBAccessRequestList{}
	err := r.List(ctx, accessRequestList, []client.ListOption{
		// client.MatchingFields{"spec.clusterName": clusterName},
		/* to be able to use this, we need to set
		      storage:
		        indices:
		        - fields:
		          - spec.clusterName
			on the CRD, but I can't, because it seems kubebuilder doesn't support adding that.
			so intead, we'll get all the resources in the cluster an iterate over them. Such is life.
			Also, with the current filter, this applies to ALL namespaces, but I don't have a way to get "namespaces that reference this particular operator"
		*/
	}...)
	if err != nil {
		logger.Error(err, "Failed to fetch access requests")
		return err
	}

	for _, accessRequest := range accessRequestList.Items {
		if accessRequest.Spec.ClusterName == clusterName {
			meta.SetStatusCondition(&accessRequest.Status.Conditions,
				metav1.Condition{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "ClusterOutOfDate",
					LastTransitionTime: metav1.NewTime(time.Now()),
					Message:            fmt.Sprintf("Cluster %s is out of date", clusterName),
				})
			r.Status().Update(ctx, &accessRequest)
		}
	}
	return nil
}

func canCreateUsers(logger logr.Logger, roles primitive.A) bool {
	/*
		https://www.mongodb.com/docs/manual/reference/built-in-roles/#superuser-roles
		https://www.mongodb.com/docs/manual/reference/built-in-roles/#mongodb-authrole-root
	*/
	var r map[string]interface{}
	for _, role := range roles {
		r = role.(map[string]interface{})
		switch r["role"] {
		case "__system": // ouch
			logger.Info("current user has __system role assigned, an internal role, assuming this is intended but not recommended, please read https://www.mongodb.com/docs/manual/reference/built-in-roles/#mongodb-authrole-__system")
			return true
		case "root", "userAdminAnyDatabase":
			return true
		}

		if r["db"] != "admin" {
			continue
		}

		if r["role"] == "dbOwner" || r["role"] == "userAdmin" {
			return true
		}
	}

	return false
}

func (r *MongoDBClusterReconciler) reconcileAtlasFirewall(ctx context.Context, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	AIRLOCK_PREFIX := "Airlock-"
	IP_ANNOTATION := "rke.cattle.io/external-ip"

	logger.Info("Reconciling atlas firewall for " + mongodbClusterCR.Name)

	client, atlasGroupID, err := getAtlasClientFromSecret(secret)
	if err != nil {
		logger.Error(err, "Couldn't get a client for Atlas")
		return err
	}

	// Get all nodes in the cluster
	nodeList := &corev1.NodeList{}

	err = r.List(ctx, nodeList)
	if err != nil {
		logger.Error(err, "Couldn't get nodes in the cluster")
		return err
	}

	// Get all nodes in the Atlas firewall
	firewallList, _, err := client.ProjectIPAccessList.List(context.Background(), atlasGroupID, nil)
	if err != nil {
		logger.Error(err, "Couldn't get nodes in the Atlas firewall")
		return err
	}

	// Look for nodes in atlas firewall that don't match the current nodes
	for _, entry := range firewallList.Results {
		found := false

		for _, node := range nodeList.Items {
			externalIP := node.Annotations[IP_ANNOTATION]

			if externalIP == entry.IPAddress && AIRLOCK_PREFIX+node.Name == entry.Comment {
				found = true
				break
			}
		}

		// If the node has the airlock prefix but wasn't found locally, remove it from the Atlas firewall
		if strings.HasPrefix(entry.Comment, AIRLOCK_PREFIX) && !found {
			logger.Info("Removing node " + entry.Comment + " from the Atlas firewall")

			_, err := client.ProjectIPAccessList.Delete(context.Background(), atlasGroupID, entry.IPAddress)
			if err != nil {
				logger.Error(err, "Couldn't remove node "+entry.Comment+" from the Atlas firewall")
				return err
			}
		}
	}

	// Add missing nodes to the Atlas firewall
	entriesToAdd := []*mongodbatlas.ProjectIPAccessList{}

	for _, node := range nodeList.Items {
		externalIP := node.Annotations[IP_ANNOTATION]

		// Check if node already exists in the firewall
		found := false

		for _, entry := range firewallList.Results {
			if externalIP == entry.IPAddress && AIRLOCK_PREFIX+node.Name == entry.Comment {
				found = true
				break
			}
		}

		// If not, add it
		if !found && externalIP != "" {
			entriesToAdd = append(entriesToAdd, &mongodbatlas.ProjectIPAccessList{
				IPAddress: externalIP,
				Comment:   AIRLOCK_PREFIX + node.Name,
			})
			logger.Info("Adding node " + node.Name + " to the Atlas firewall")
		}
	}

	_, response, err := client.ProjectIPAccessList.Create(context.Background(), atlasGroupID, entriesToAdd)
	if err != nil || response.StatusCode != http.StatusCreated {
		logger.Error(err, "Couldn't add nodes to the Atlas firewall")
		return err
	}

	return nil
}
