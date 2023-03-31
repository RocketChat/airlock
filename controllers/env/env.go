package env

import (
	"os"
	"strconv"
)

var FORCE_USER_UPDATE bool
var DBCLUSTER_NAMESPACE string

func init() {
	FORCE_USER_UPDATE, _ = strconv.ParseBool(getEnv("FORCE_USER_UPDATE", "false"))

	DBCLUSTER_NAMESPACE = getEnv("DBCLUSTER_NAMESPACE", "airlock-system")
}

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}
