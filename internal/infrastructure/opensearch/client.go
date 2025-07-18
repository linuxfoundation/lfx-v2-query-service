// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

type httpClient struct {
	baseURL    string
	httpClient *http.Client
	client     *opensearchapi.Client
}

func (c *httpClient) Search(ctx context.Context, index string, query []byte) (*SearchResponse, error) {

	slog.DebugContext(ctx, "executing opensearch search",
		"index", index,
		"query", string(query),
	)

	searchRequest := opensearchapi.SearchReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
		Params: opensearchapi.SearchParams{
			Source: true,
			SourceIncludes: []string{
				"object_ref",
				"object_type",
				"object_id",
				"public",
				"access_check_object",
				"access_check_relation",
				"data",
			},
		},
	}

	searchResponse, errSearchResponse := c.client.Search(ctx, &searchRequest)
	if errSearchResponse != nil {
		return nil, fmt.Errorf("failed to execute search: %w", errSearchResponse)
	}

	// Check for errors in the response
	if searchResponse.Errors {
		return nil, fmt.Errorf("opensearch search returned errors")
	}

	result := &SearchResponse{
		Hits: Hits{
			Total: Total{
				Value: searchResponse.Hits.Total.Value,
			},
			Hits: make([]Hit, len(searchResponse.Hits.Hits)),
		},
	}
	for i, hit := range searchResponse.Hits.Hits {
		result.Hits.Hits[i] = Hit{
			ID:     hit.ID,
			Source: hit.Source,
		}
	}

	// if the number of hits returned equals the page size, there may be more results.
	if len(searchResponse.Hits.Hits) == constants.DefaultPageSize {
		searchAfter := searchResponse.Hits.Hits[len(searchResponse.Hits.Hits)-1].Sort
		pageToken, errEncodePageToken := paging.EncodePageToken(searchAfter, global.PageTokenSecret(ctx))
		if errEncodePageToken != nil {
			slog.ErrorContext(ctx, "failed to encode page token", "error", errEncodePageToken)
			return nil, errEncodePageToken
		}
		result.PageToken = &pageToken
		slog.DebugContext(ctx, "pagination token generated",
			"page_token", *result.PageToken,
			"total_hits", searchResponse.Hits.Total.Value,
		)
	}

	return result, nil
}
