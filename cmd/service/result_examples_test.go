// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedCountResultExamplesUseSeparateModes(t *testing.T) {
	source, err := os.ReadFile("../../gen/http/openapi3.json")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(source, &doc))
	object := func(value any) map[string]any {
		m, ok := value.(map[string]any)
		require.True(t, ok, "expected JSON object, got %T", value)
		return m
	}
	at := func(keys ...string) map[string]any {
		var value any = doc
		for _, key := range keys {
			value = object(value)[key]
		}
		return object(value)
	}
	media := at("paths", "/query/resources/count", "get", "responses", "200", "content", "application/json")
	examples := object(media["examples"])
	require.Len(t, examples, 2)
	require.Contains(t, examples, "grouped")
	require.Contains(t, examples, "cardinality")
	require.NotContains(t, media, "example", "named examples must replace the combined success example")
	assertMode := func(example map[string]any) {
		require.Contains(t, example, "count")
		require.Contains(t, example, "has_more")
		hasGroup := false
		hasMetric := false
		for _, key := range []string{"groups", "groups_complete", "group_count_error_upper_bound"} {
			if _, ok := example[key]; ok {
				hasGroup = true
			}
		}
		for _, key := range []string{"metric_value", "metric_complete"} {
			if _, ok := example[key]; ok {
				hasMetric = true
			}
		}
		require.NotEqual(t, hasGroup, hasMetric, "response example must contain exactly one aggregation mode")
	}
	grouped := object(object(examples["grouped"])["value"])
	cardinality := object(object(examples["cardinality"])["value"])
	assertMode(grouped)
	assertMode(cardinality)
	require.Contains(t, grouped, "groups")
	require.Contains(t, grouped, "groups_complete")
	require.EqualValues(t, 0, grouped["group_count_error_upper_bound"])
	require.Contains(t, cardinality, "metric_value")
	require.Contains(t, cardinality, "metric_complete")
	// Schema-level examples must also avoid the impossible combined body.
	schema := at("components", "schemas", "QueryResourcesCountResponseBody")
	if example, ok := schema["example"]; ok {
		assertMode(object(example))
	}
}
