// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	pkgerrors "github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestMappingRevalidation(t *testing.T) {
	keyword := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}, "tags": {Type: "keyword"}, "data": {Type: "flat_object"}}}
	text := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "text", Fields: map[string]FieldMapping{"keyword": {Type: "keyword"}}}, "tags": {Type: "keyword"}, "data": {Type: "flat_object"}}}
	unsupported := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "text"}}}
	t.Run("cache expires at five minutes and adopts a changed field", func(t *testing.T) {
		logs := captureLogs(t)
		client := NewMockOpenSearchClient()
		client.mappingResponse = IndexMappings{"a": keyword, "b": keyword}
		clock := time.Unix(100, 0)
		searcher := &OpenSearchSearcher{client: client, index: "alias", now: func() time.Time { return clock }}
		field, err := searcher.resolveAccessKeyField(context.Background())
		require.NoError(t, err)
		require.Equal(t, accessCheckQueryField, field)
		require.Equal(t, 1, client.mappingCalls)
		clock = clock.Add(5*time.Minute - time.Nanosecond)
		field, err = searcher.resolveAccessKeyField(context.Background())
		require.NoError(t, err)
		require.Equal(t, accessCheckQueryField, field)
		require.Equal(t, 1, client.mappingCalls, "no re-read inside the interval")
		clock = clock.Add(time.Nanosecond)
		field, err = searcher.resolveAccessKeyField(context.Background())
		require.NoError(t, err)
		require.Equal(t, accessCheckQueryField, field)
		require.Equal(t, 2, client.mappingCalls, "re-read at the exact boundary")
		require.Equal(t, 1, strings.Count(logs.String(), `"msg":"resolved access key field"`), "unchanged revalidation must not emit another Info")
		client.mappingResponse = IndexMappings{"a": text, "b": text}
		clock = clock.Add(5 * time.Minute)
		field, err = searcher.resolveAccessKeyField(context.Background())
		require.NoError(t, err)
		require.Equal(t, accessCheckQueryKeywordField, field)
		require.Equal(t, 3, client.mappingCalls)
		require.Equal(t, 2, strings.Count(logs.String(), `"msg":"resolved access key field"`), "changed field is logged at Info")
	})
	for _, tc := range []struct {
		name     string
		mappings IndexMappings
		err      error
	}{
		{"failed read", IndexMappings{"a": keyword}, errors.New("mapping unavailable")},
		{"unsupported field", IndexMappings{"a": unsupported}, nil},
		{"missing field", IndexMappings{"a": IndexMapping{}}, nil},
		{"new disagreeing alias target", IndexMappings{"a": keyword, "b": text}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewMockOpenSearchClient()
			client.mappingResponse = IndexMappings{"a": keyword}
			clock := time.Unix(100, 0)
			searcher := &OpenSearchSearcher{client: client, index: "alias", now: func() time.Time { return clock }}
			_, err := searcher.resolveAccessKeyField(context.Background())
			require.NoError(t, err)
			client.mappingResponse = tc.mappings
			client.SetMappingError(tc.err)
			clock = clock.Add(5 * time.Minute)
			_, err = searcher.AccessBuckets(context.Background(), model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 100})
			var unavailable pkgerrors.ServiceUnavailable
			require.ErrorAs(t, err, &unavailable)
			require.Empty(t, searcher.accessKeyField, "expired field cannot survive failed revalidation")
			require.Equal(t, 2, client.mappingCalls)
			require.Zero(t, client.aggregationCalls, "never query the stale field")
			// Repair immediately, but the shared negative-cache window must still apply.
			client.SetMappingError(nil)
			client.mappingResponse = IndexMappings{"a": text, "b": text}
			clock = clock.Add(30*time.Second - time.Nanosecond)
			_, err = searcher.resolveAccessKeyField(context.Background())
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, 2, client.mappingCalls)
			clock = clock.Add(time.Nanosecond)
			field, err := searcher.resolveAccessKeyField(context.Background())
			require.NoError(t, err)
			require.Equal(t, accessCheckQueryKeywordField, field)
			require.Equal(t, 3, client.mappingCalls)
		})
	}
}
