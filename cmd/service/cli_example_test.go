// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	client "github.com/linuxfoundation/lfx-v2-query-service/gen/http/query_svc/client"
	"github.com/stretchr/testify/require"
)

func TestGeneratedCountCLIExampleUsesOneMode(t *testing.T) {
	// Pin the generated usage, not a separately handwritten example. Goa emits
	// all flags, and its optional-string builder treats --metric "" as absent.
	source, err := os.ReadFile("../../gen/http/cli/lfx_v2_query_service/cli.go")
	require.NoError(t, err)
	start := strings.Index(string(source), "func querySvcQueryResourcesCountUsage()")
	require.NotEqual(t, -1, start)
	usage := strings.SplitN(string(source)[start:], "\nfunc ", 2)[0]
	parts := strings.SplitN(usage, "Example:", 2)
	require.Len(t, parts, 2)
	flags := map[string]string{}
	pattern := regexp.MustCompile(`--([a-z-]+) ("(?:\\.|[^"\\])*"|'[^']*'|[^\s]+)`)
	for _, match := range pattern.FindAllStringSubmatch(parts[1], -1) {
		value := match[2]
		if strings.HasPrefix(value, `"`) {
			value, err = strconv.Unquote(value)
			require.NoError(t, err)
		} else if strings.HasPrefix(value, "'") {
			value = strings.Trim(value, "'")
		}
		flags[match[1]] = value
	}
	require.Equal(t, "project_uid", flags["group-by"])
	require.Contains(t, flags, "metric")
	require.Empty(t, flags["metric"])
	payload, err := client.BuildQueryResourcesCountPayload(
		flags["version"], flags["name"], flags["parent"], flags["type"], flags["tags"], flags["tags-all"],
		flags["date-field"], flags["date-from"], flags["date-to"], flags["filters"], flags["filters-all"], flags["filters-or"],
		flags["group-by"], flags["group-by-size"], flags["metric"], flags["bearer-token"],
	)
	require.NoError(t, err)
	require.Nil(t, payload.Metric, "empty example flag must not enable metric mode")
	svc := &querySvcsrvc{}
	agg, err := svc.payloadToCountAggregation(payload)
	require.NoError(t, err)
	require.Equal(t, "project_uid", agg.GroupByPrefix)
	require.Empty(t, agg.CardinalityPrefix)
	_, err = svc.payloadToCountPublicCriteria(payload)
	require.NoError(t, err)
}
