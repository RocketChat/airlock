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

	"github.com/RocketChat/airlock/controllers/env"
	"github.com/RocketChat/airlock/controllers/hash"
	"github.com/thanhpk/randstr"
	"go.mongodb.org/atlas/mongodbatlas"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

// MongoDBAccessRequestReconciler reconciles a MongoDBAccessRequest object
type MongoDBAccessRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	airlockFinalizer = "airlock.cloud.rocket.chat/finalizer"
)

//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=airlock.cloud.rocket.chat,resources=mongodbaccessrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups="";apps;networking.k8s.io,resources=secrets,verbs=get;list;watch;create;update;patch;delete

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
		logger.Info("Operator resource object not found. It was probably deleted")
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Error getting operator resource object")

		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "CustomResourceNotReady",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("unable to get operator custom resource: %s", err.Error()),
			})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	mongodbClusterCR := &airlockv1alpha1.MongoDBCluster{}

	err = r.generateAttributes(ctx, mongodbAccessRequestCR)
	if err != nil {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "AttributeGenerationFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Attribute generation failed with error: %s", err.Error()),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	err = r.Get(ctx, types.NamespacedName{Namespace: "", Name: mongodbAccessRequestCR.Spec.ClusterName}, mongodbClusterCR)
	if err != nil {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "GetMongoDBClusterFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Failed to get MongoDBCluster resource for %s: %s", mongodbAccessRequestCR.Spec.ClusterName, err.Error()),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	clusterSecret := &corev1.Secret{}

	err = r.Get(ctx, types.NamespacedName{Namespace: mongodbClusterCR.Spec.ConnectionSecretNamespace, Name: mongodbClusterCR.Spec.ConnectionSecret}, clusterSecret)
	if err != nil {
		meta.SetStatusCondition(&mongodbClusterCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "SecretReadFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Failed to read cluster connection secret %s: %s", mongodbClusterCR.Spec.ConnectionSecret, err.Error()),
			})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbClusterCR)})
	}

	// ### Finalizer logic ###
	// examine DeletionTimestamp to determine if object is under deletion
	if mongodbAccessRequestCR.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(mongodbAccessRequestCR, airlockFinalizer) {
			controllerutil.AddFinalizer(mongodbAccessRequestCR, airlockFinalizer)

			if err := r.Update(ctx, mongodbAccessRequestCR); err != nil {
				logger.Error(err, "Failed to add finalizer.")
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(mongodbAccessRequestCR, airlockFinalizer) {
			// our finalizer is present, so lets cleanup our created user on mongo/atlas
			if mongodbClusterCR.Spec.UseAtlasApi {
				err = r.cleanupAtlasUser(ctx, mongodbAccessRequestCR, mongodbClusterCR, clusterSecret)
				if err != nil {
					logger.Error(err, "Cleanup failed for atlas.")
					if isStatusReady(mongodbAccessRequestCR) {
						meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
							metav1.Condition{
								Type:               "Ready",
								Status:             metav1.ConditionFalse,
								Reason:             "DeleteAtlasUserFailed",
								LastTransitionTime: metav1.NewTime(time.Now()),
								Message:            fmt.Sprintf("Failed to delete atlas user while cleaning resource. You may need to manually remove the finalizer: %s", err.Error()),
							})

						return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
					} else {
						logger.Info("Ignoring cleanup failed as the resource was not ready")
					}
				}
			} else {
				err = r.cleanupMongoUser(ctx, mongodbAccessRequestCR, mongodbClusterCR, clusterSecret)
				if err != nil {
					logger.Error(err, "Cleanup failed for mongodb.")
					if isStatusReady(mongodbAccessRequestCR) {
						meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
							metav1.Condition{
								Type:               "Ready",
								Status:             metav1.ConditionFalse,
								Reason:             "DeleteMongoUserFailed",
								LastTransitionTime: metav1.NewTime(time.Now()),
								Message:            fmt.Sprintf("Failed to delete mongo user while cleaning resource. You may need to manually remove the finalizer: %s", err.Error()),
							})

						return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
					} else {
						logger.Info("Ignoring cleanup failed as the resource was not ready")
					}
				}
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(mongodbAccessRequestCR, airlockFinalizer)
			if err := r.Update(ctx, mongodbAccessRequestCR); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// We need to try to read the password from the secret before anything, because if it exists, we need to use it
	// If it doesn't exist yet, one will be generated
	userPassword, err := r.readPasswordOrGenerate(ctx, req, mongodbAccessRequestCR)
	if err != nil {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "GetSecretFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Failed to get secret resource for %s: %s", mongodbAccessRequestCR.Spec.SecretName, err.Error()),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	// TODO: does this need sanitization?
	connectionString := fmt.Sprintf("%s://%s:%s@%s/%s%s",
		mongodbClusterCR.Spec.PrefixTemplate,
		mongodbAccessRequestCR.Spec.UserName,
		userPassword,
		mongodbClusterCR.Spec.HostTemplate,
		mongodbAccessRequestCR.Spec.Database,
		mongodbClusterCR.Spec.OptionsTemplate)

	err = r.reconcileSecret(ctx, req, mongodbAccessRequestCR, connectionString, userPassword)
	if err != nil {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "SecretUpdateFailed",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Unable to update secret %q: %s", req.NamespacedName, err.Error()),
			})

		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	if mongodbClusterCR.Spec.UseAtlasApi {
		err = r.reconcileAtlasUser(ctx, mongodbAccessRequestCR, mongodbClusterCR, clusterSecret, connectionString, userPassword)
		if err != nil {
			meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
				metav1.Condition{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "UpdateAtlasFailed",
					LastTransitionTime: metav1.NewTime(time.Now()),
					Message:            fmt.Sprintf("Failed to create or update user on atlas: %s", err.Error()),
				})

			return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
		}
	} else {
		err = r.reconcileMongoDBUser(ctx, mongodbAccessRequestCR, mongodbClusterCR, clusterSecret, connectionString, userPassword)
		if err != nil {
			meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
				metav1.Condition{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "UpdateMongoFailed",
					LastTransitionTime: metav1.NewTime(time.Now()),
					Message:            fmt.Sprintf("Failed to create or update user on mongodb: %s", err.Error()),
				})

			return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
		}
	}

	// If we are already Ready == true, dont update it again
	if !isStatusReady(mongodbAccessRequestCR) {
		meta.SetStatusCondition(&mongodbAccessRequestCR.Status.Conditions,
			metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "AccessRequestGranted",
				LastTransitionTime: metav1.NewTime(time.Now()),
				Message:            fmt.Sprintf("Access request granted for %s", mongodbAccessRequestCR.Name),
			})
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, mongodbAccessRequestCR)})
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MongoDBAccessRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&airlockv1alpha1.MongoDBAccessRequest{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

// Read password from secret
func (r *MongoDBAccessRequestReconciler) readPasswordOrGenerate(ctx context.Context, req ctrl.Request, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest) (string, error) {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: mongodbAccessRequestCR.Spec.SecretName}, secret)

	if err != nil && errors.IsNotFound(err) {
		logger.Info("Secret " + mongodbAccessRequestCR.Spec.SecretName + " not found.")

		return randstr.String(16), nil
	} else if err != nil {
		logger.Error(err, "Error getting secret to read password from.")

		return "", err
	}
	if string(secret.Data["password"]) == "" {
		logger.Info("Password not found in secret " + mongodbAccessRequestCR.Spec.SecretName + ". Generating a new one.")
		return randstr.String(16), nil
	}
	return string(secret.Data["password"]), nil
}

func (r *MongoDBAccessRequestReconciler) reconcileSecret(ctx context.Context, req ctrl.Request, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, connectionString string, password string) error {
	logger := log.FromContext(ctx)

	connectionSecret := &corev1.Secret{}
	create := false

	logger.Info("Reconciling secret for " + mongodbAccessRequestCR.Name)

	err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: mongodbAccessRequestCR.Spec.SecretName}, connectionSecret)

	if err != nil && errors.IsNotFound(err) {
		create = true

		connectionSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mongodbAccessRequestCR.Spec.SecretName,
				Namespace: req.Namespace,
			},
			Data: map[string][]byte{
				"connectionString": []byte(connectionString),
				"password":         []byte(password),
			},
		}
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("Error getting existing secret: %q", req.NamespacedName))
		return err
	}

	actualHash := hash.Object(connectionSecret.Data)

	connectionSecret.Data["connectionString"] = []byte(connectionString)
	connectionSecret.Data["password"] = []byte(password)

	_ = ctrl.SetControllerReference(mongodbAccessRequestCR, connectionSecret, r.Scheme)
	expectedHash := hash.Object(connectionSecret.Data)
	if create {
		logger.Info(fmt.Sprintf("creating secret %q: expected hash: %q", req.NamespacedName, expectedHash))

		err = r.Create(ctx, connectionSecret)
	} else if expectedHash != actualHash {
		logger.Info(fmt.Sprintf("updating secret %q: expected hash %q does not match actual hash %q", req.NamespacedName, expectedHash, actualHash))

		err = r.Update(ctx, connectionSecret)
	}
	if err != nil {
		return err
	}
	return nil
}

func (r *MongoDBAccessRequestReconciler) generateAttributes(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest) error {
	changed := false

	if mongodbAccessRequestCR.Spec.Database == "" {
		mongodbAccessRequestCR.Spec.Database = mongodbAccessRequestCR.Name
		changed = true
	}

	if mongodbAccessRequestCR.Spec.UserName == "" {
		mongodbAccessRequestCR.Spec.UserName = mongodbAccessRequestCR.Name
		changed = true
	}

	if mongodbAccessRequestCR.Spec.SecretName == "" {
		mongodbAccessRequestCR.Spec.SecretName = mongodbAccessRequestCR.Name
		changed = true
	}

	if changed {
		err := r.Update(ctx, mongodbAccessRequestCR)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *MongoDBAccessRequestReconciler) cleanupMongoUser(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, clusterSecret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	logger.Info("Cleaning up MongoDB user " + mongodbAccessRequestCR.Spec.UserName)

	connectionString, err := getSecretProperty(clusterSecret, "connectionString")
	if err != nil {
		return err
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return err
	}

	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			logger.Error(err, "Error disconnecting from MongoDB")
		}
	}()

	result := client.Database(mongodbAccessRequestCR.Spec.Database).RunCommand(context.Background(), bson.D{{Key: "dropUser", Value: mongodbAccessRequestCR.Spec.UserName}})
	if result.Err() != nil && !strings.Contains(result.Err().Error(), "UserNotFound") {
		logger.Error(result.Err(), "Error deleting MongoDB user")

		return result.Err()
	}

	logger.Info("Successfully deleted MongoDB user "+mongodbAccessRequestCR.Spec.UserName, " on ", mongodbAccessRequestCR.Spec.Database)

	return nil
}

func (r *MongoDBAccessRequestReconciler) reconcileMongoDBUser(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, clusterSecret *corev1.Secret, userConnectionString string, userPassword string) error {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling MongoDB user")

	connectionString, err := getSecretProperty(clusterSecret, "connectionString")
	if err != nil {
		return err
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return err
	}

	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			logger.Error(err, "Error disconnecting from MongoDB")
		}
	}()

	// Connect to the database as created user
	userClient, err := mongo.Connect(ctx, options.Client().ApplyURI(userConnectionString))

	// Test if the user access is working
	err = userClient.Ping(ctx, readpref.Primary())
	userClient.Disconnect(ctx)
	// if error is null. This means that if the user access is working, we don't need to do anything
	if err == nil && !env.FORCE_USER_UPDATE {
		return nil
	}
	if env.FORCE_USER_UPDATE {
		logger.Info("Force updating user " + mongodbAccessRequestCR.Spec.UserName + " on cluster " + mongodbAccessRequestCR.Spec.ClusterName)
	} else {
		logger.Info("User access is not working, creating user " + mongodbAccessRequestCR.Spec.UserName + " on cluster " + mongodbAccessRequestCR.Spec.ClusterName)
	}
	// Create the user
	result := client.Database(mongodbAccessRequestCR.Spec.Database).RunCommand(context.Background(), bson.D{{Key: "createUser", Value: mongodbAccessRequestCR.Spec.UserName},
		{Key: "pwd", Value: userPassword}, {Key: "roles", Value: []bson.M{{"role": "readWrite", "db": mongodbAccessRequestCR.Spec.Database}}}})
	if result.Err() != nil {
		if strings.Contains(result.Err().Error(), "already exists") {
			// If user already exists, ensure the password is correct
			logger.Info("User " + mongodbAccessRequestCR.Spec.UserName + " already exists, updating password")
			result = client.Database(mongodbAccessRequestCR.Spec.Database).RunCommand(context.Background(), bson.D{{Key: "updateUser", Value: mongodbAccessRequestCR.Spec.UserName},
				{Key: "pwd", Value: userPassword}})
			if result.Err() != nil {
				logger.Error(result.Err(), "Error updating MongoDB user")
				return result.Err()
			}
			return nil
		}
		logger.Error(result.Err(), "Error creating MongoDB user")
		return result.Err()
	}
	logger.Info("Successfully created MongoDB user "+mongodbAccessRequestCR.Spec.UserName, " on ", mongodbAccessRequestCR.Spec.Database)

	return nil
}

// OBS: this is not called when a user is renamed in the CR, thus not cleaning the old one up. Should we worry about this?
func (r *MongoDBAccessRequestReconciler) cleanupAtlasUser(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, clusterSecret *corev1.Secret) error {
	logger := log.FromContext(ctx)

	logger.Info("Cleaning up Atlas user " + mongodbAccessRequestCR.Spec.UserName)

	client, atlasGroupID, err := getAtlasClientFromSecret(clusterSecret)
	if err != nil {
		logger.Error(err, "Couldn't get a client for Atlas")
		return err
	}

	result, err := client.DatabaseUsers.Delete(ctx, "admin", atlasGroupID, mongodbAccessRequestCR.Spec.UserName)
	if err != nil && result.StatusCode != http.StatusNotFound {
		logger.Error(err, "Failed to delete atlas user "+mongodbAccessRequestCR.Spec.UserName)
		return err
	}

	return nil
}

func (r *MongoDBAccessRequestReconciler) reconcileAtlasUser(ctx context.Context, mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest, mongodbClusterCR *airlockv1alpha1.MongoDBCluster, clusterSecret *corev1.Secret, userConnectionString string, userPassword string) error {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling Atlas user")

	client, atlasGroupID, err := getAtlasClientFromSecret(clusterSecret)
	if err != nil {
		logger.Error(err, "Couldn't get a client for Atlas")
		return err
	}

	// Connect to the database as created user
	userClient, err := mongo.Connect(ctx, options.Client().ApplyURI(userConnectionString))
	if err != nil {
		return err
	}

	// Test if the user access is working
	err = userClient.Ping(ctx, readpref.Primary())
	_ = userClient.Disconnect(ctx)

	// if error is null. This means that if the user access is working, we don't need to do anything
	if err == nil {
		return nil
	}

	// Create the user
	databaseUser := mongodbatlas.DatabaseUser{
		DatabaseName: "admin",
		Username:     mongodbAccessRequestCR.Spec.UserName,
		Password:     userPassword,
		GroupID:      atlasGroupID,
		Roles: []mongodbatlas.Role{
			{
				RoleName:     "readWrite",
				DatabaseName: mongodbAccessRequestCR.Spec.Database,
			},
		},
	}

	_, result, err := client.DatabaseUsers.Create(ctx, databaseUser.GroupID, &databaseUser)
	if err != nil {
		// If user already exists, update it
		if result.StatusCode == http.StatusConflict {
			logger.Info("User " + mongodbAccessRequestCR.Spec.UserName + " already exists, updating password")

			_, _, err = client.DatabaseUsers.Update(ctx, databaseUser.GroupID, databaseUser.Username, &databaseUser)
			if err != nil {
				logger.Error(err, "Error updating Atlas user")
				return err
			}

			return nil
		}

		logger.Error(err, "Error creating atlas user "+mongodbAccessRequestCR.Spec.UserName)

		return err
	}

	logger.Info("Successfully created MongoDB user "+mongodbAccessRequestCR.Spec.UserName, " on ", mongodbAccessRequestCR.Spec.Database)

	return nil
}

func isStatusReady(mongodbAccessRequestCR *airlockv1alpha1.MongoDBAccessRequest) bool {
	if len(mongodbAccessRequestCR.Status.Conditions) == 0 || mongodbAccessRequestCR.Status.Conditions[0].Status != metav1.ConditionTrue {
		return false
	} else {
		return true
	}
}
