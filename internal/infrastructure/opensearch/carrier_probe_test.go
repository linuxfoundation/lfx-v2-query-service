// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSearchSearcherTypeCarries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		probe  model.CarrierProbe
		clause string
	}{
		{"any document", model.CarrierProbe{Kind: model.AnyDocument}, ""},
		{"data field", model.CarrierProbe{Kind: model.DataField, Name: "start_time"}, `,{"exists":{"field":"data.start_time"}}`},
		{"parent kind", model.CarrierProbe{Kind: model.ParentKind, Name: "project"}, `,{"prefix":{"parent_refs":"project:"}}`},
		{"tag prefix", model.CarrierProbe{Kind: model.TagPrefix, Name: "category"}, `,{"prefix":{"tags":"category:"}}`},
		{"escaped field", model.CarrierProbe{Kind: model.DataField, Name: "a\"\x01"}, `,{"exists":{"field":"data.a\"\u0001"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, total := range []int{0, 1, 2} {
				t.Run(fmt.Sprint(total), func(t *testing.T) {
					client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
						assert.Equal(t, "/resources/_search", r.URL.Path)
						assert.Equal(t, "false", r.URL.Query().Get("allow_partial_search_results"))
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						assert.JSONEq(t, `{"size":0,"terminate_after":1,"track_total_hits":1,"query":{"bool":{"filter":[{"term":{"object_type":"committee"}}`+tc.clause+`]}}}`, string(body))
						w.Header().Set("Content-Type", "application/json")
						_, _ = fmt.Fprintf(w, `{"timed_out":false,"terminated_early":%t,"_shards":{"total":2,"successful":2,"failed":0},"hits":{"total":{"value":%d,"relation":"gte"},"hits":[]}}`, total > 0, total)
					})
					searcher := &OpenSearchSearcher{client: client, index: "resources"}
					carried, err := searcher.TypeCarries(context.Background(), "committee", tc.probe)
					require.NoError(t, err)
					assert.Equal(t, total > 0, carried)
				})
			}
		})
	}

	for _, tc := range []struct {
		name     string
		status   int
		response string
	}{
		{"timeout", 200, `{"timed_out":true,"hits":{"total":{"value":0}}}`},
		{"failed shard", 200, `{"_shards":{"failed":1},"hits":{"total":{"value":0}}}`},
		{"unavailable", 503, `{"error":{"type":"unavailable","reason":"unavailable"},"status":503}`},
		{"invalid JSON", 200, `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			})
			searcher := &OpenSearchSearcher{client: client, index: "resources"}
			carried, err := searcher.TypeCarries(context.Background(), "committee", model.CarrierProbe{Kind: model.AnyDocument})
			require.Error(t, err)
			assert.False(t, carried)
		})
	}

	t.Run("invalid kind does not query", func(t *testing.T) {
		searcher := &OpenSearchSearcher{}
		_, err := searcher.TypeCarries(context.Background(), "committee", model.CarrierProbe{Kind: "invalid"})
		require.ErrorContains(t, err, "unsupported carrier probe kind")
	})

	t.Run("type is JSON escaped", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			filters := body["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
			assert.Equal(t, "type\"\x01", filters[0].(map[string]any)["term"].(map[string]any)["object_type"])
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hits":{"total":{"value":0}}}`))
		})
		searcher := &OpenSearchSearcher{client: client, index: "resources"}
		_, err := searcher.TypeCarries(context.Background(), "type\"\x01", model.CarrierProbe{Kind: model.AnyDocument})
		require.NoError(t, err)
	})
}
