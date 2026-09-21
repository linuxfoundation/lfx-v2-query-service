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
	"github.com/stretchr/testify/require"
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

func TestHTTPClientSearchCursor(t *testing.T) {
	t.Setenv("PAGE_TOKEN_SECRET", "12345678901234567890123456789012") // 32 chars
	fullPage := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":3},"hits":[` +
		`{"_id":"a-1","_source":{"object_type":"project_membership","object_id":"m-1","data":{"uid":"m-1"}},"sort":["a corp","a-1"]},` +
		`{"_id":"b-1","_source":{"object_type":"project_membership","object_id":"m-2","data":{"uid":"m-2"}},"sort":["b corp","b-1"]}]}}`

	tests := []struct {
		name         string
		body         string
		pageSize     int
		expectedSort []string
		expectCursor *string
	}{
		{
			name:         "a full sorted page keeps every hit's cursor and continues from the last",
			body:         fullPage,
			pageSize:     2,
			expectedSort: []string{`["a corp","a-1"]`, `["b corp","b-1"]`},
			expectCursor: func() *string { s := `["b corp","b-1"]`; return &s }(),
		},
		{
			name:         "a short page keeps every hit's cursor and carries no continuation",
			body:         fullPage,
			pageSize:     3,
			expectedSort: []string{`["a corp","a-1"]`, `["b corp","b-1"]`},
			expectCursor: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})
			response, err := client.Search(context.Background(), "resources", []byte(`{"query":{"match_all":{}}}`), tc.pageSize)
			assert.NoError(t, err)
			assert.Len(t, response.Hits.Hits, len(tc.expectedSort))
			for i, hit := range response.Hits.Hits {
				assert.JSONEq(t, tc.expectedSort[i], string(hit.Sort), "hit %d keeps its own sort values", i)
			}
			if tc.expectCursor == nil {
				assert.Nil(t, response.SearchAfter)
				assert.Nil(t, response.PageToken)
				return
			}
			assert.NotNil(t, response.SearchAfter)
			assert.JSONEq(t, *tc.expectCursor, *response.SearchAfter)
			assert.NotNil(t, response.PageToken, "a full page also carries the opaque token for callers")
		})
	}
}

// TestHTTPClientSearchMintsCursorWithToken verifies that a full page yields both
// the opaque page token and the raw search_after cursor it encodes (the JSON
// sort values of the last hit), and that a short page yields neither.
func TestHTTPClientSearchMintsCursorWithToken(t *testing.T) {
	t.Setenv("PAGE_TOKEN_SECRET", "12345678901234567890123456789012")
	body := `{"hits":{"total":{"value":3},"hits":[` +
		`{"_id":"a","_score":1,"_source":{"object_id":"a"},"sort":["alpha","a"]},` +
		`{"_id":"b","_score":1,"_source":{"object_id":"b"},"sort":["beta","b"]}]}}`

	t.Run("full page", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
		resp, err := client.Search(context.Background(), "resources", []byte(`{"size":2}`), 2)
		require.NoError(t, err)
		require.NotNil(t, resp.PageToken)
		require.NotNil(t, resp.SearchAfter)
		assert.JSONEq(t, `["beta","b"]`, *resp.SearchAfter, "cursor is the last hit's sort values")
	})

	t.Run("short page", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
		resp, err := client.Search(context.Background(), "resources", []byte(`{"size":5}`), 5)
		require.NoError(t, err)
		assert.Nil(t, resp.PageToken)
		assert.Nil(t, resp.SearchAfter)
	})
}
