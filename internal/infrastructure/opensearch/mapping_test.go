// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	pkgerrors "github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestResolveAliasMappings(t *testing.T) {
	keyword := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}}
	text := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "text", Fields: map[string]FieldMapping{"keyword": {Type: "keyword"}}}}}
	unusable := IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "text"}}}
	for _, tc := range []struct {
		name     string
		mappings IndexMappings
		field    string
		fail     bool
	}{
		{"keyword agreement", IndexMappings{"a": keyword, "b": keyword}, accessCheckQueryField, false},
		{"text agreement", IndexMappings{"a": text, "b": text}, accessCheckQueryKeywordField, false},
		{"different supported fields", IndexMappings{"a": keyword, "b": text}, "", true},
		{"one unusable field", IndexMappings{"a": keyword, "b": unusable}, "", true},
		{"both unusable fields", IndexMappings{"a": unusable, "b": unusable}, "", true},
		{"single unusable field", IndexMappings{"a": unusable}, "", true},
		{"single missing field", IndexMappings{"a": IndexMapping{}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewMockOpenSearchClient()
			client.mappingResponse = tc.mappings
			clock := time.Unix(100, 0)
			searcher := &OpenSearchSearcher{client: client, index: "alias", now: func() time.Time { return clock }}
			field, err := searcher.resolveAccessKeyField(context.Background())
			if tc.fail {
				var unavailable pkgerrors.ServiceUnavailable
				require.ErrorAs(t, err, &unavailable)
				require.Empty(t, searcher.accessKeyField)
				// A failed alias resolution shares the same negative-cache window as a failed read.
				_, err = searcher.AccessBuckets(context.Background(), model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 100})
				require.ErrorAs(t, err, &unavailable)
				require.Equal(t, 1, client.mappingCalls)
				require.Zero(t, client.aggregationCalls)
				clock = clock.Add(accessKeyFieldRetryInterval)
				client.mappingResponse = IndexMappings{"a": keyword, "b": keyword}
				field, err = searcher.resolveAccessKeyField(context.Background())
				require.NoError(t, err)
				require.Equal(t, accessCheckQueryField, field)
				require.Equal(t, 2, client.mappingCalls)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.field, field)
			}
			before := client.mappingCalls
			_, err = searcher.resolveAccessKeyField(context.Background())
			require.NoError(t, err)
			require.Equal(t, before, client.mappingCalls, "all-index success is memoized")
		})
	}
}
