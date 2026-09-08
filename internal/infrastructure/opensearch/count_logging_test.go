// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/require"
)

func TestCountQueriesDoNotLogValues(t *testing.T) {
	logs := captureLogs(t)
	client := NewMockOpenSearchClient()
	client.SetCountResponse(&CountResponse{Count: 1})
	client.SetAggregationResponses(
		map[string]any{"access_keys": map[string]any{"buckets": []any{}}},
		map[string]any{"group_by": map[string]any{"buckets": []any{}}},
	)
	searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}
	sentinel := "sensitive-marker@example.com"
	criteria := model.SearchCriteria{Name: &sentinel, TagsAll: []string{"email:" + sentinel}, PageSize: -1, PublicOnly: true}
	_, err := searcher.CountPublic(context.Background(), criteria)
	require.NoError(t, err)
	criteria.PublicOnly = false
	criteria.PrivateOnly = true
	_, err = searcher.AccessBuckets(context.Background(), criteria, model.AccessBucketRequest{PageSize: 100, After: &sentinel})
	require.NoError(t, err)
	criteria.PrivateOnly = false
	_, err = searcher.AuthorizedAggregation(context.Background(), criteria, model.CountAggregation{IncludePublic: true, AuthorizedKeys: []string{sentinel}, GroupByPrefix: "email", GroupBySize: 10})
	require.NoError(t, err)
	require.NotEmpty(t, logs.String())
	require.NotContains(t, logs.String(), sentinel)
	require.Contains(t, logs.String(), `"authorized_key_count":1`)
}
