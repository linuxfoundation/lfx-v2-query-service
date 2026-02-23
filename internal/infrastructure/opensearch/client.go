// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

type httpClient struct {
	baseURL string
	client  *opensearchapi.Client
}

func (c *httpClient) Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error) {

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
	if pageSize > 0 && len(searchResponse.Hits.Hits) == pageSize {
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

func (c *httpClient) AggregationSearch(ctx context.Context, index string, query []byte) (*AggregationResponse, error) {
	searchRequest := opensearchapi.SearchReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
	}

	// Perform the search.
	searchResponse, err := c.client.Search(ctx, &searchRequest)
	if err != nil {
		return nil, fmt.Errorf("opensearch search failed: %w", err)
	}

	if searchResponse.Errors {
		return nil, fmt.Errorf("opensearch search returned errors")
	}

	// First, unmarshal the aggregations from raw JSON.
	var aggregations AggregationResponse
	if err := json.Unmarshal(searchResponse.Aggregations, &aggregations); err != nil {
		slog.ErrorContext(ctx, "failed to unmarshal aggregations", "error", err)
		return nil, fmt.Errorf("unrecoverable aggregation processing error: %w", err)
	}

	return &aggregations, nil
}

func (c *httpClient) Count(ctx context.Context, index string, query []byte) (*CountResponse, error) {
	countRequest := opensearchapi.IndicesCountReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
	}
	countResponse, err := c.client.Indices.Count(ctx, &countRequest)
	if err != nil {
		return nil, fmt.Errorf("opensearch count failed: %w", err)
	}
	return &CountResponse{
		Count: countResponse.Count,
	}, nil
}

func (c *httpClient) IsReady(ctx context.Context) error {
	pingReq := &opensearchapi.PingReq{
		Params: opensearchapi.PingParams{
			ErrorTrace: false,
		},
	}

	resp, err := c.client.Ping(ctx, pingReq)
	if err != nil {
		return errors.NewServiceUnavailable("opensearch client is not ready", err)
	}
	defer func() {
		if resp.Body != nil {
			errClose := resp.Body.Close()
			if errClose != nil {
				slog.ErrorContext(ctx, "failed to close response body", "error", errClose)
			}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "opensearch is not ready", "status_code", resp.StatusCode)
		return errors.NewServiceUnavailable("opensearch is not ready", fmt.Errorf("status code: %d", resp.StatusCode))
	}
	return nil
}
