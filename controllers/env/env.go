package env

import (
	"io/ioutil"
	"os"
	"strconv"
)

var FORCE_USER_UPDATE bool
var OPERATOR_NAMESPACE string

func init() {
	FORCE_USER_UPDATE, _ = strconv.ParseBool(getEnv("FORCE_USER_UPDATE", "false"))

	var err error
	serviceAccountNamepace, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	// Try to get the operator namespace from the service account, if that fails, fallback to an environment variable.
	if err != nil {
		OPERATOR_NAMESPACE = getEnv("OPERATOR_NAMESPACE", "airlock-system")
	} else {
		OPERATOR_NAMESPACE = string(serviceAccountNamepace)
	}
}

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}
