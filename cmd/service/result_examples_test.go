// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedCountResultExamplesUseSeparateModes(t *testing.T) {
	source, err := os.ReadFile("../../gen/http/openapi3.json")
	require.NoError(t, err)
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber() // Do not round uint64 examples through float64.
	require.NoError(t, decoder.Decode(&doc))
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
	require.Equal(t, json.Number("0"), grouped["group_count_error_upper_bound"])
	require.Contains(t, cardinality, "metric_value")
	require.Contains(t, cardinality, "metric_complete")
	// Schema-level examples must also avoid the impossible combined body.
	schema := at("components", "schemas", "QueryResourcesCountResponseBody")
	if example, ok := schema["example"]; ok {
		assertMode(object(example))
	}
}

func TestGeneratedCountResultExamplesFitInt64(t *testing.T) {
	for _, filename := range []string{"openapi.json", "openapi3.json"} {
		t.Run(filename, func(t *testing.T) {
			source, err := os.ReadFile("../../gen/http/" + filename)
			require.NoError(t, err)
			var doc map[string]any
			decoder := json.NewDecoder(bytes.NewReader(source))
			decoder.UseNumber() // MaxInt64 and MaxInt64+1 must remain distinguishable.
			require.NoError(t, decoder.Decode(&doc))
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
			resolve := func(ref string) map[string]any {
				require.True(t, strings.HasPrefix(ref, "#/"), "expected local schema reference")
				return at(strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
			}
			var checkExample func(any)
			checkExample = func(value any) {
				switch v := value.(type) {
				case json.Number:
					n, err := strconv.ParseUint(v.String(), 10, 64)
					require.NoError(t, err, "count examples must be unsigned integers: %s", v)
					require.LessOrEqual(t, n, uint64(math.MaxInt64), "example exceeds the declared int64 format: %s", v)
				case map[string]any:
					hasGroup, hasMetric := false, false
					for key, child := range v {
						hasGroup = hasGroup || strings.HasPrefix(key, "groups") || key == "group_count_error_upper_bound"
						hasMetric = hasMetric || strings.HasPrefix(key, "metric")
						checkExample(child)
					}
					require.False(t, hasGroup && hasMetric, "schema/synthesized example mixes grouped and metric modes")
				case []any:
					for _, child := range v {
						checkExample(child)
					}
				}
			}
			// Inspect only response examples, not unrelated API schemas or schema
			// metadata. Follow refs so CountGroup and its nested examples are covered.
			seen := map[string]bool{}
			var walkSchema func(any)
			walkSchema = func(value any) {
				switch v := value.(type) {
				case map[string]any:
					if ref, ok := v["$ref"].(string); ok && !seen[ref] {
						seen[ref] = true
						walkSchema(resolve(ref))
					}
					for key, child := range v {
						if key == "example" || key == "examples" {
							checkExample(child)
						} else {
							walkSchema(child)
						}
					}
				case []any:
					for _, child := range v {
						walkSchema(child)
					}
				}
			}
			response := at("paths", "/query/resources/count", "get", "responses", "200")
			walkSchema(response)
			body := response
			if filename == "openapi3.json" {
				body = object(object(response["content"])["application/json"])
			}
			schema := resolve(object(body["schema"])["$ref"].(string))
			properties := object(schema["properties"])
			for field, want := range map[string]string{"count": "42", "group_count_error_upper_bound": "0", "metric_value": "17"} {
				require.Equal(t, json.Number(want), object(properties[field])["example"], "pin %s instead of letting Goa randomize it", field)
			}
			groupRef := object(object(properties["groups"])["items"])["$ref"].(string)
			groupCount := object(object(resolve(groupRef)["properties"])["count"])
			require.Equal(t, json.Number("30"), groupCount["example"])
		})
	}
}

func TestGeneratedMembershipSummaryResultExampleRenders(t *testing.T) {
	source, err := os.ReadFile("../../gen/http/openapi3.json")
	require.NoError(t, err)
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber() // Do not round uint64 examples through float64.
	require.NoError(t, decoder.Decode(&doc))
	object := func(value any) map[string]any {
		m, ok := value.(map[string]any)
		require.True(t, ok, "expected JSON object, got %T", value)
		return m
	}
	array := func(value any) []any {
		a, ok := value.([]any)
		require.True(t, ok, "expected JSON array, got %T", value)
		return a
	}
	at := func(keys ...string) map[string]any {
		var value any = doc
		for _, key := range keys {
			value = object(value)[key]
		}
		return object(value)
	}
	response := at("paths", "/query/memberships/summary", "get", "responses", "200")
	media := object(object(response["content"])["application/json"])
	require.Contains(t, media, "example", "the named result example must reach the response body")
	example := object(media["example"])

	// The example shows one shape: the summaries of one organization on one
	// project, the records folded into them, and a whole read.
	require.Equal(t, json.Number("2"), example["terms_total"])
	require.Equal(t, true, example["complete"])
	summaries := array(example["summaries"])
	require.Len(t, summaries, 1)
	summary := object(summaries[0])
	for _, field := range []string{
		"b2b_org_uid", "company_name", "project_uid", "project_slug", "term_count",
		"first_start", "last_end", "current_status", "current_tier_name",
		"current_start", "current_end", "current_membership_uid",
		"tier_names", "statuses", "terms",
	} {
		require.Contains(t, summary, field, "the example must show every summary attribute")
	}
	terms := array(summary["terms"])
	require.Equal(t, json.Number(strconv.Itoa(len(terms))), summary["term_count"],
		"term_count must agree with the records shown")
	require.Equal(t, json.Number(strconv.Itoa(len(terms))), example["terms_total"],
		"terms_total must agree with the records shown")
	require.Equal(t, summary["current_membership_uid"], object(terms[len(terms)-1])["membership_uid"],
		"the current record must be one of the records shown")
	require.Equal(t, summary["first_start"], object(terms[0])["start_date"])
	require.Equal(t, summary["last_end"], object(terms[len(terms)-1])["end_date"])

	// Goa renders UInt64 as int64 in OpenAPI; the scalar samples stay in range.
	for _, sample := range []any{example["terms_total"], summary["term_count"]} {
		number, ok := sample.(json.Number)
		require.True(t, ok, "expected a JSON number, got %T", sample)
		value, errParse := strconv.ParseUint(number.String(), 10, 64)
		require.NoError(t, errParse)
		require.LessOrEqual(t, value, uint64(math.MaxInt64))
	}

	// The response also carries the cache-control header of a public read.
	header := object(object(response["headers"])["Cache-Control"])
	require.Contains(t, header, "example")
}
