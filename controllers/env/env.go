package env

import (
	"os"
	"strconv"
)

var FORCE_USER_UPDATE bool

func init() {
	FORCE_USER_UPDATE, _ = strconv.ParseBool(getEnv("FORCE_USER_UPDATE", "false"))
}

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}
