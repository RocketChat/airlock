package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-logr/logr"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/log"

	airlockv1alpha1 "github.com/RocketChat/airlock/api/v1alpha1"
)

type connectionStringParts struct {
	Prefix     string
	Hosts      string
	Options    string
	ReplicaSet string
}

func parseConnectionString(uri string) (connectionStringParts, error) {
	cs, err := connstring.Parse(uri)
	if err != nil {
		return connectionStringParts{}, fmt.Errorf("parse connection string: %w", err)
	}

	parts := connectionStringParts{
		Prefix:     cs.Scheme,
		Hosts:      strings.Join(cs.Hosts, ","),
		ReplicaSet: cs.ReplicaSet,
	}

	if parsed, err := url.Parse(uri); err == nil && parsed.RawQuery != "" {
		parts.Options = "?" + parsed.RawQuery
	}

	return parts, nil
}

func effectivePrefix(prefixTemplate string) string {
	if prefixTemplate == "" {
		return connstring.SchemeMongoDB
	}

	return prefixTemplate
}

func selectedConnectionParts(srvParts, stdParts connectionStringParts, prefix string) (connectionStringParts, error) {
	switch prefix {
	case connstring.SchemeMongoDBSRV:
		if srvParts.Hosts == "" {
			return connectionStringParts{}, fmt.Errorf("Atlas cluster has no SRV connection string")
		}

		return srvParts, nil
	case connstring.SchemeMongoDB:
		if stdParts.Hosts == "" {
			return connectionStringParts{}, fmt.Errorf("Atlas cluster has no standard connection string")
		}

		return stdParts, nil
	default:
		return connectionStringParts{}, fmt.Errorf("invalid prefix %q", prefix)
	}
}

func needsAtlasConnectionPopulate(spec airlockv1alpha1.MongoDBClusterSpec) bool {
	if spec.AtlasClusterName == "" {
		return false
	}

	return spec.HostTemplate == "" || spec.OptionsTemplate == "" || spec.PrefixTemplate == ""
}

func hostTemplateMatches(hostTemplate, atlasHosts string) bool {
	if hostTemplate == atlasHosts {
		return true
	}

	return strings.Contains(atlasHosts, hostTemplate)
}

func parseQueryParams(options string) map[string]string {
	params := map[string]string{}
	options = strings.TrimPrefix(options, "?")
	if options == "" {
		return params
	}

	for _, pair := range strings.Split(options, "&") {
		if pair == "" {
			continue
		}

		key, value, _ := strings.Cut(pair, "=")
		params[key] = value
	}

	return params
}

func logOptionsDiff(logger logr.Logger, clusterName, userOptions, atlasOptions string) {
	userParams := parseQueryParams(userOptions)
	atlasParams := parseQueryParams(atlasOptions)

	for key, atlasValue := range atlasParams {
		userValue, ok := userParams[key]
		if !ok {
			logger.Info("optionsTemplate differs from Atlas: parameter missing in CR",
				"cluster", clusterName, "parameter", key, "atlasValue", atlasValue)
			continue
		}

		if userValue != atlasValue {
			logger.Info("optionsTemplate differs from Atlas: parameter value mismatch",
				"cluster", clusterName, "parameter", key, "crValue", userValue, "atlasValue", atlasValue)
		}
	}

	for key, userValue := range userParams {
		if _, ok := atlasParams[key]; !ok {
			logger.Info("optionsTemplate differs from Atlas: extra parameter in CR",
				"cluster", clusterName, "parameter", key, "crValue", userValue)
		}
	}
}

func (r *MongoDBClusterReconciler) reconcileAtlasClusterConnectionDetails(ctx context.Context, cr *airlockv1alpha1.MongoDBCluster, secret *corev1.Secret) (bool, error) {
	logger := log.FromContext(ctx)

	legacyClient, groupID, err := getAtlasClientFromSecret(secret)
	if err != nil {
		return false, err
	}

	clusterName, err := resolveAtlasClusterName(ctx, cr.Spec, legacyClient, groupID)
	if err != nil {
		return false, err
	}

	adminClient, _, err := getAtlasAdminClientFromSecret(secret)
	if err != nil {
		return false, err
	}

	cluster, resp, err := adminClient.ClustersApi.GetCluster(ctx, groupID, clusterName).Execute()
	if err != nil {
		return false, fmt.Errorf("fetch Atlas cluster %q: %w", clusterName, err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch Atlas cluster %q: HTTP %s", clusterName, resp.Status)
	}

	var srvParts, stdParts connectionStringParts

	if cluster.ConnectionStrings != nil {
		if cluster.ConnectionStrings.StandardSrv != nil && *cluster.ConnectionStrings.StandardSrv != "" {
			srvParts, err = parseConnectionString(*cluster.ConnectionStrings.StandardSrv)
			if err != nil {
				return false, fmt.Errorf("parse SRV connection string: %w", err)
			}
		}

		if cluster.ConnectionStrings.Standard != nil && *cluster.ConnectionStrings.Standard != "" {
			stdParts, err = parseConnectionString(*cluster.ConnectionStrings.Standard)
			if err != nil {
				return false, fmt.Errorf("parse standard connection string: %w", err)
			}
		}
	}

	if needsAtlasConnectionPopulate(cr.Spec) {
		prefix := cr.Spec.PrefixTemplate
		if prefix == "" {
			prefix = connstring.SchemeMongoDBSRV
		}

		parts, err := selectedConnectionParts(srvParts, stdParts, prefix)
		if err != nil {
			return false, err
		}

		updated := false

		if cr.Spec.HostTemplate == "" {
			cr.Spec.HostTemplate = parts.Hosts
			updated = true
		}

		if cr.Spec.OptionsTemplate == "" {
			cr.Spec.OptionsTemplate = parts.Options
			updated = true
		}

		if cr.Spec.PrefixTemplate == "" {
			cr.Spec.PrefixTemplate = parts.Prefix
			updated = true
		}

		if updated {
			if err := r.Client.Update(ctx, cr); err != nil {
				return false, fmt.Errorf("update MongoDBCluster spec: %w", err)
			}

			logger.Info("Populated Atlas connection details from cluster", "cluster", clusterName)

			return true, nil
		}
	}

	prefix := effectivePrefix(cr.Spec.PrefixTemplate)
	if prefix != connstring.SchemeMongoDB && prefix != connstring.SchemeMongoDBSRV {
		return false, errors.NewBadRequest(fmt.Sprintf("invalid prefixTemplate %q", prefix))
	}

	parts, err := selectedConnectionParts(srvParts, stdParts, prefix)
	if err != nil {
		return false, err
	}

	if parts.Prefix != prefix {
		return false, errors.NewBadRequest(fmt.Sprintf("prefixTemplate %q does not match Atlas connection string prefix %q", prefix, parts.Prefix))
	}

	if cr.Spec.HostTemplate != "" && !hostTemplateMatches(cr.Spec.HostTemplate, parts.Hosts) {
		return false, errors.NewBadRequest(fmt.Sprintf("hostTemplate %q does not match Atlas host(s) %q for %s connection", cr.Spec.HostTemplate, parts.Hosts, prefix))
	}

	if cr.Spec.OptionsTemplate != "" {
		logOptionsDiff(logger, cr.Name, cr.Spec.OptionsTemplate, parts.Options)
	}

	return false, nil
}
