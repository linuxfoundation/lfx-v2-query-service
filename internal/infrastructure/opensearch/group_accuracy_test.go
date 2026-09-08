// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/require"
)

func TestGroupedCountAccuracyDecode(t *testing.T) {
	// Exercise the actual HTTP decoder and adapter with a multi-shard response.
	// Truncation and count error must remain separate signals, including when
	// every group is present but OpenSearch reports potential undercounts.
	for _, tc := range []struct {
		name         string
		bound, other uint64
	}{
		{"all groups exact", 0, 0},
		{"all groups count uncertain", 7, 0},
		{"truncated groups exact returned counts", 0, 11},
		{"truncated groups and uncertain counts", 7, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"timed_out":false,"_shards":{"total":3,"successful":3,"failed":0},"aggregations":{"group_by":{"doc_count_error_upper_bound":%d,"sum_other_doc_count":%d,"buckets":[{"key":"project_uid:P1","doc_count":30}]}}}`, tc.bound, tc.other)
			})
			searcher := &OpenSearchSearcher{client: client, index: "resources", accessKeyField: accessCheckQueryField}
			result, err := searcher.AuthorizedAggregation(context.Background(), model.SearchCriteria{}, model.CountAggregation{IncludePublic: true, GroupByPrefix: "project_uid", GroupBySize: 1})
			require.NoError(t, err)
			require.Equal(t, tc.bound, result.GroupCountErrorUpperBound)
			require.Equal(t, tc.other == 0, result.GroupsComplete, "completeness is not accuracy")
			require.Equal(t, []model.CountGroup{{Key: "P1", Count: 30}}, result.Groups)
		})
	}
}
