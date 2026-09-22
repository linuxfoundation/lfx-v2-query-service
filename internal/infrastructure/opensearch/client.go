// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
	opensearch "github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

type httpClient struct {
	baseURL string
	client  *opensearchapi.Client
}

// Search runs one page of a query and returns its hits with their cursors.
//
// Partial results are refused, as on AggregationSearch: a page missing a
// shard's hits would be a shorter page, and a short page is what ends a
// paged read. The plain search would mint a cursor past the hits it never
// saw, and the membership summary would report a read complete that was
// not, so a response with a failed shard or a timeout is an error, never a
// smaller page. allow_partial_search_results=false makes OpenSearch fail
// the request instead of returning a partial 200; the _shards/timed_out
// check covers engines that ignore the parameter.
func (c *httpClient) Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error) {

	slog.DebugContext(ctx, "executing opensearch search",
		"index", index,
		"query", string(query),
	)

	allowPartial := false
	searchRequest := opensearchapi.SearchReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
		Params: opensearchapi.SearchParams{
			AllowPartialSearchResults: &allowPartial,
			Source:                    true,
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
		return nil, requestFailure(ctx, "failed to execute search", errSearchResponse)
	}

	// Check for errors in the response
	if searchResponse.Errors {
		return nil, fmt.Errorf("opensearch search returned errors")
	}
	if searchResponse.Timeout || searchResponse.Shards.Failed > 0 {
		return nil, errors.NewServiceUnavailable("opensearch returned a partial search result",
			fmt.Errorf("timed_out=%t shards_failed=%d", searchResponse.Timeout, searchResponse.Shards.Failed))
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
		if len(hit.Sort) > 0 {
			// Each hit keeps its own cursor, so a service-side read can
			// resume from any hit, not only from the last of a page.
			sortValues, errSortValues := json.Marshal(hit.Sort)
			if errSortValues != nil {
				slog.ErrorContext(ctx, "failed to encode hit sort values", "error", errSortValues)
				return nil, errSortValues
			}
			result.Hits.Hits[i].Sort = sortValues
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
		cursor, errCursor := json.Marshal(searchAfter)
		if errCursor != nil {
			slog.ErrorContext(ctx, "failed to encode search_after cursor", "error", errCursor)
			return nil, fmt.Errorf("failed to encode search_after cursor: %w", errCursor)
		}
		cursorStr := string(cursor)
		result.SearchAfter = &cursorStr
		slog.DebugContext(ctx, "pagination token generated",
			"page_token", *result.PageToken,
			"total_hits", searchResponse.Hits.Total.Value,
		)
	}

	return result, nil
}

// AggregationSearch runs a size-0 search and returns the aggregations
// member of the response as raw JSON so each caller can unmarshal the
// aggregation shape it asked for.
//
// Partial results are refused: the count route derives "exhaustive" from
// what this call returns (a short composite page ends a walk; an empty
// group set is reported complete), so a response missing a shard's data
// must be an error, not a smaller answer. allow_partial_search_results=false
// makes OpenSearch fail the request instead of returning a partial 200; the
// _shards/timed_out check covers engines that ignore the parameter.
func (c *httpClient) AggregationSearch(ctx context.Context, index string, query []byte) (json.RawMessage, error) {
	allowPartial := false
	searchRequest := opensearchapi.SearchReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
		Params: opensearchapi.SearchParams{
			AllowPartialSearchResults: &allowPartial,
		},
	}

	// Perform the search.
	searchResponse, err := c.client.Search(ctx, &searchRequest)
	if err != nil {
		return nil, requestFailure(ctx, "opensearch search failed", err)
	}

	if searchResponse.Errors {
		return nil, fmt.Errorf("opensearch search returned errors")
	}
	if searchResponse.Timeout || searchResponse.Shards.Failed > 0 {
		return nil, errors.NewServiceUnavailable("opensearch returned a partial aggregation result",
			fmt.Errorf("timed_out=%t shards_failed=%d", searchResponse.Timeout, searchResponse.Shards.Failed))
	}

	return searchResponse.Aggregations, nil
}

// GetMapping returns every backing index's properties, preserving the index
// names so the searcher can validate agreement before selecting a field.
func (c *httpClient) GetMapping(ctx context.Context, index string) (IndexMappings, error) {
	mappingResponse, err := c.client.Indices.Mapping.Get(ctx, &opensearchapi.MappingGetReq{
		Indices: []string{index},
	})
	if err != nil {
		return nil, fmt.Errorf("opensearch get mapping failed: %w", err)
	}
	if len(mappingResponse.Indices) == 0 {
		return nil, fmt.Errorf("opensearch get mapping returned no index for %q", index)
	}
	mappings := make(IndexMappings, len(mappingResponse.Indices))
	for name, entry := range mappingResponse.Indices {
		var mapping IndexMapping
		if err := json.Unmarshal(entry.Mappings, &mapping); err != nil {
			return nil, fmt.Errorf("failed to unmarshal index mapping: %w", err)
		}
		mappings[name] = mapping
	}
	return mappings, nil
}

func (c *httpClient) Count(ctx context.Context, index string, query []byte) (*CountResponse, error) {
	countRequest := opensearchapi.IndicesCountReq{
		Indices: []string{index},
		Body:    bytes.NewReader(query),
	}
	countResponse, err := c.client.Indices.Count(ctx, &countRequest)
	if err != nil {
		return nil, requestFailure(ctx, "opensearch count failed", err)
	}
	// _count has no allow_partial_search_results; a failed shard means the
	// number is a lower bound, which the count route must not present as exact.
	if countResponse.Shards.Failed > 0 {
		return nil, errors.NewServiceUnavailable("opensearch returned a partial count",
			fmt.Errorf("shards_failed=%d", countResponse.Shards.Failed))
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

// requestFailure classifies a failed OpenSearch request. A clause-limit
// rejection is the caller's to fix. A request OpenSearch could not answer
// whole is service unavailable: with allow_partial_search_results=false a
// failed shard or a timeout comes back as a 5xx error response rather than
// a partial 200, and a request that never reached OpenSearch is the same
// outage. A request the caller's own context ended, and any other error
// response, is the service's own request at fault.
func requestFailure(ctx context.Context, operation string, err error) error {
	var structErr *opensearch.StructError
	if stderrors.As(err, &structErr) {
		if hasTooManyClauses(structErr) {
			return errors.NewValidation("query exceeds the OpenSearch maximum clause limit: reduce the number of filter values", err)
		}
		if structErr.Status < http.StatusInternalServerError {
			return fmt.Errorf("%s: %w", operation, err)
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return errors.NewServiceUnavailable(operation, err)
}

// hasTooManyClauses checks whether a StructError was caused by too_many_nested_clauses.
// OpenSearch surfaces this inside a search_phase_execution_exception root_cause.
func hasTooManyClauses(e *opensearch.StructError) bool {
	for _, rc := range e.Err.RootCause {
		if rc.Type == "too_many_nested_clauses" {
			return true
		}
	}
	return false
}
