package controllers

import (
	"context"
	"strings"

	"github.com/mongodb-forks/digest"
	"go.mongodb.org/atlas/mongodbatlas"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

func getSecretProperty(secret *corev1.Secret, property string) (string, error) {
	value := string(secret.Data[property])
	if value == "" {
		err := errors.NewServiceUnavailable(property + " not found in secret " + secret.Name)
		return value, err
	}

	return value, nil
}

func getAtlasClientFromSecret(secret *corev1.Secret) (*mongodbatlas.Client, string, error) {
	atlasPublicKey, err := getSecretProperty(secret, "atlasPublicKey")
	if err != nil {
		return nil, "", err
	}

	atlasPrivateKey, err := getSecretProperty(secret, "atlasPrivateKey")
	if err != nil {
		return nil, "", err
	}

	atlasGroupID, err := getSecretProperty(secret, "atlasGroupID")
	if err != nil {
		return nil, "", err
	}

	t := digest.NewTransport(atlasPublicKey, atlasPrivateKey)

	tc, err := t.Client()
	if err != nil {
		return nil, "", err
	}

	client := mongodbatlas.NewClient(tc)

	return client, atlasGroupID, nil
}

func getClusterNameFromHostTemplate(ctx context.Context, client *mongodbatlas.Client, groupID, hostTemplate string) (string, error) {
	clusters, _, err := client.Clusters.List(ctx, groupID, &mongodbatlas.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, cluster := range clusters {
		// Check for the host template in both SrvAddress (mongo+srv:// connection) and MongoURI (legacy replicaset uri with the 3 RS menbers)
		if strings.Contains(cluster.SrvAddress, hostTemplate) {
			return cluster.Name, nil
		}

		if strings.Contains(cluster.MongoURI, hostTemplate) {
			return cluster.Name, nil
		}
	}

	return "", errors.NewBadRequest("Cluster not found when searching for it's connectionString in atlas")
}

// getDatabaseSize is used to calculate the volume size required for the backup job
func getDatabaseSize(ctx context.Context, connectionString, database string, collections []string) (int64, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)

	db := client.Database(database)
	var totalSize int64

	if len(collections) == 0 {
		// Get all collections in the database
		collectionNames, err := db.ListCollectionNames(ctx, map[string]interface{}{})
		if err != nil {
			return 0, err
		}
		collections = collectionNames
	}

	// Calculate size for each collection
	for _, collectionName := range collections {
		// Get collection stats using the collStats command
		var result struct {
			Size int64 `bson:"size"`
		}

		err := db.RunCommand(ctx, bson.M{
			"collStats": collectionName,
		}).Decode(&result)

		if err != nil {
			// Collection might not exist, skip it
			continue
		}

		totalSize += result.Size
	}

	return totalSize, nil
}
