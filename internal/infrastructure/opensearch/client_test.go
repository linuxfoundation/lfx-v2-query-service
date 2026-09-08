// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	"github.com/stretchr/testify/assert"
)

// newTestClient points an httpClient at an httptest server.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*httpClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	apiClient, err := opensearchapi.NewClient(opensearchapi.Config{Client: opensearch.Config{Addresses: []string{server.URL}}})
	if err != nil {
		t.Fatalf("opensearchapi.NewClient: %v", err)
	}
	return &httpClient{baseURL: server.URL, client: apiClient}, server
}

func TestHTTPClientAggregationSearch(t *testing.T) {
	t.Run("returns the raw aggregations and refuses partial results", func(t *testing.T) {
		var gotQuery string
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0}},"aggregations":{"access_keys":{"buckets":[]}}}`))
		})
		raw, err := client.AggregationSearch(context.Background(), "resources", []byte(`{"size":0}`))
		assert.NoError(t, err)
		assert.JSONEq(t, `{"access_keys":{"buckets":[]}}`, string(raw))
		assert.Contains(t, gotQuery, "allow_partial_search_results=false")
	})

	t.Run("a failed shard is service unavailable, not a smaller answer", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"took":1,"timed_out":false,"_shards":{"total":2,"successful":1,"failed":1,"failures":[{"shard":1,"reason":{"type":"circuit_breaking_exception"}}]},"hits":{"total":{"value":0}},"aggregations":{"access_keys":{"buckets":[]}}}`))
		})
		_, err := client.AggregationSearch(context.Background(), "resources", []byte(`{"size":0}`))
		var unavailable errors.ServiceUnavailable
		assert.True(t, stderrors.As(err, &unavailable), "got %v", err)
	})

	t.Run("a timed out search is service unavailable", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"took":1,"timed_out":true,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0}},"aggregations":{}}`))
		})
		_, err := client.AggregationSearch(context.Background(), "resources", []byte(`{"size":0}`))
		var unavailable errors.ServiceUnavailable
		assert.True(t, stderrors.As(err, &unavailable), "got %v", err)
	})

	t.Run("too many clauses is a validation error", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"root_cause":[{"type":"too_many_nested_clauses","reason":"too many"}],"type":"search_phase_execution_exception","reason":"all shards failed"},"status":400}`))
		})
		_, err := client.AggregationSearch(context.Background(), "resources", []byte(`{"size":0}`))
		var validation errors.Validation
		assert.True(t, stderrors.As(err, &validation), "got %v", err)
	})
}

func TestHTTPClientCount(t *testing.T) {
	t.Run("returns the count", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"count":42,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0}}`))
		})
		response, err := client.Count(context.Background(), "resources", []byte(`{"query":{"match_all":{}}}`))
		assert.NoError(t, err)
		assert.Equal(t, 42, response.Count)
	})

	t.Run("a failed shard is service unavailable, not a lower bound", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"count":42,"_shards":{"total":2,"successful":1,"skipped":0,"failed":1}}`))
		})
		_, err := client.Count(context.Background(), "resources", []byte(`{"query":{"match_all":{}}}`))
		var unavailable errors.ServiceUnavailable
		assert.True(t, stderrors.As(err, &unavailable), "got %v", err)
	})
}

func TestHTTPClientGetMapping(t *testing.T) {
	t.Run("reads the single index's properties", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resources":{"mappings":{"properties":{"access_check_query":{"type":"keyword"},"tags":{"type":"keyword"}}}}}`))
		})
		mapping, err := client.GetMapping(context.Background(), "resources")
		assert.NoError(t, err)
		assert.Equal(t, "keyword", mapping["resources"].Properties["access_check_query"].Type)
	})

	t.Run("no index in the response is an error", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		_, err := client.GetMapping(context.Background(), "resources")
		assert.Error(t, err)
	})

	t.Run("an alias preserves every backing index mapping", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resources-a":{"mappings":{"properties":{"access_check_query":{"type":"keyword"}}}},"resources-b":{"mappings":{"properties":{"access_check_query":{"type":"keyword"}}}}}`))
		})
		mapping, err := client.GetMapping(context.Background(), "resources")
		assert.NoError(t, err)
		assert.Len(t, mapping, 2)
		assert.Equal(t, "keyword", mapping["resources-a"].Properties["access_check_query"].Type)
		assert.Equal(t, "keyword", mapping["resources-b"].Properties["access_check_query"].Type)
	})

	t.Run("an HTTP error is an error", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"type":"security_exception","reason":"no permissions"},"status":403}`))
		})
		_, err := client.GetMapping(context.Background(), "resources")
		assert.Error(t, err)
	})
}
