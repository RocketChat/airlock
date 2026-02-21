package controllers

import (
	"context"
	"fmt"
	"math"

	"go.mongodb.org/mongo-driver/bson"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getDatabaseSize is used to calculate the volume size required for the backup job
func getDatabaseSize(ctx context.Context, connectionString, database string) (int64, error) {
	logger := log.FromContext(ctx)

	logger.Info("trying to estimate db size", "database", database)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)

	db := client.Database(database)

	logger.Info("running dbStats against db", "database", database)

	// Run dbStats command to get database size
	var result bson.M
	err = db.RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&result)
	if err != nil {
		return 0, err
	}

	// Extract dataSize from the result
	dataSize, ok := result["dataSize"]
	if !ok {
		return 0, fmt.Errorf("dataSize not found in dbStats result")
	}

	logger.Info("dbSize response", "database", database, "size", dataSize)

	// Convert to int64 (dataSize can be int32 or int64)
	switch v := dataSize.(type) {
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(math.Ceil(v)), nil
	default:
		return 0, fmt.Errorf("unexpected dataSize type: %T", dataSize)
	}
}
