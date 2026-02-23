// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"text/template"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"

	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var queryResourceTemplate = template.Must(
	template.New("queryResource").
		Funcs(template.FuncMap{
			"quote": strconv.Quote,
		}).
		Parse(queryResourceSource))

// OpenSearchSearcher implements the ResourceSearcher interface for OpenSearch
type OpenSearchSearcher struct {
	client OpenSearchClientRetriever
	index  string
}

// OpenSearchClientRetriever defines the interface for OpenSearch operations
// This allows for easy mocking and testing
type OpenSearchClientRetriever interface {
	Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error)
	Count(ctx context.Context, index string, query []byte) (*CountResponse, error)
	AggregationSearch(ctx context.Context, index string, query []byte) (*AggregationResponse, error)
	IsReady(ctx context.Context) error
}

// QueryResources implements the ResourceSearcher interface
func (os *OpenSearchSearcher) QueryResources(ctx context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {
	slog.DebugContext(ctx, "executing opensearch query for criteria",
		"criteria", criteria,
	)

	// Render the appropriate query template
	query, err := os.Render(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to render query: %w", err)
	}

	// Execute the search
	response, err := os.client.Search(ctx, os.index, query, criteria.PageSize)
	if err != nil {
		return nil, fmt.Errorf("opensearch search failed: %w", err)
	}

	// Convert response to domain objects
	result, err := os.convertSearchResponse(ctx, response)
	if err != nil {
		return nil, fmt.Errorf("failed to convert search response: %w", err)
	}

	slog.DebugContext(ctx, "opensearch search completed",
		"results_count", len(result.Resources),
	)
	return result, nil
}

func (os *OpenSearchSearcher) QueryResourcesCount(
	ctx context.Context,
	publicCountCriteria model.SearchCriteria,
	aggregationCriteria model.SearchCriteria,
	publicOnly bool,
) (*model.CountResult, error) {
	slog.DebugContext(ctx, "executing opensearch query for criteria",
		"public_count_criteria", publicCountCriteria,
		"aggregation_criteria", aggregationCriteria,
	)

	parsedCount, err := os.Render(ctx, publicCountCriteria)
	if err != nil {
		// Not expected to happen: this is an error with our interpolation logic.
		slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
		return nil, fmt.Errorf("failed to render query: %w", err)
	}
	slog.DebugContext(ctx, "public resource count query", "query", string(parsedCount))

	// Execute the search
	countResponse, err := os.client.Count(ctx, os.index, parsedCount)
	if err != nil {
		return nil, fmt.Errorf("opensearch search failed: %w", err)
	}

	if publicOnly {
		return &model.CountResult{
			Count: countResponse.Count,
		}, nil
	}

	parsedSearch, err := os.Render(ctx, aggregationCriteria)
	if err != nil {
		// Not expected to happen: this is an error with our interpolation logic.
		slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
		return nil, fmt.Errorf("failed to render query: %w", err)
	}
	slog.DebugContext(ctx, "resource aggregation query", "query", string(parsedSearch))

	aggregationResponse, err := os.client.AggregationSearch(ctx, os.index, parsedSearch)
	if err != nil {
		return nil, fmt.Errorf("opensearch search failed: %w", err)
	}

	slog.DebugContext(ctx, "aggregation response", "response", aggregationResponse)

	result, err := os.convertCountResponse(countResponse, aggregationResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to convert count response: %w", err)
	}

	slog.DebugContext(ctx, "converted count response", "response", result)

	return result, nil
}

// Render generates the OpenSearch query based on the provided search criteria
func (os *OpenSearchSearcher) Render(ctx context.Context, criteria model.SearchCriteria) ([]byte, error) {
	var buf bytes.Buffer
	if err := queryResourceTemplate.Execute(&buf, criteria); err != nil {
		slog.ErrorContext(ctx, "failed to render query template", "error", err)
		return nil, err
	}
	query := json.RawMessage(buf.Bytes())

	parsed, err := json.Marshal(query)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal rendered query", "error", err)
		return nil, err
	}
	return parsed, nil
}

// convertResponse converts OpenSearch response to domain objects
func (os *OpenSearchSearcher) convertSearchResponse(ctx context.Context, response *SearchResponse) (*model.SearchResult, error) {

	result := &model.SearchResult{
		Resources: make([]model.Resource, 0, len(response.Hits.Hits)),
		PageToken: response.PageToken,
		Total:     response.Value,
	}

	for _, hit := range response.Hits.Hits {
		resource, err := os.convertHit(hit)
		if err != nil {
			// Log error but continue processing other hits
			slog.ErrorContext(ctx, "failed to convert hit", "hitid", hit.ID, "error", err)
			continue
		}
		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

// convertHit converts a single OpenSearch hit to a domain resource
func (os *OpenSearchSearcher) convertHit(hit Hit) (model.Resource, error) {
	resource := model.Resource{
		ID: hit.ID,
	}

	// Parse the source data
	if hit.Source != nil {
		sourceData := make(map[string]any)
		if err := json.Unmarshal(hit.Source, &sourceData); err != nil {
			return resource, fmt.Errorf("failed to unmarshal source data: %w", err)
		}

		// Extract type
		if typeVal, ok := sourceData["object_type"].(string); ok {
			resource.Type = typeVal
		}

		// Extract data
		data, ok := sourceData["data"]
		if !ok {
			// If no separate data field, use the entire source as data
			data = sourceData
		}
		resource.Data = data

		if err := json.Unmarshal(hit.Source, &resource.TransactionBodyStub); err != nil {
			return resource, fmt.Errorf("failed to unmarshal source data into TransactionBodyStub: %w", err)
		}

	}

	return resource, nil
}

func (os *OpenSearchSearcher) convertCountResponse(response *CountResponse, aggregationResponse *AggregationResponse) (*model.CountResult, error) {
	aggregation := model.TermsAggregation{
		DocCountErrorUpperBound: aggregationResponse.GroupBy.DocCountErrorUpperBound,
		SumOtherDocCount:        aggregationResponse.GroupBy.SumOtherDocCount,
	}
	aggregationBuckets := make([]model.AggregationBucket, len(aggregationResponse.GroupBy.Buckets))
	for i, bucket := range aggregationResponse.GroupBy.Buckets {
		aggregationBuckets[i] = model.AggregationBucket{
			Key:      bucket.Key,
			DocCount: bucket.DocCount,
		}
	}
	aggregation.Buckets = aggregationBuckets
	return &model.CountResult{
		Count:       response.Count,
		Aggregation: aggregation,
	}, nil
}

func (o *OpenSearchSearcher) IsReady(ctx context.Context) error {
	if err := o.client.IsReady(ctx); err != nil {
		slog.ErrorContext(ctx, "opensearch client is not ready", "error", err)
		return err
	}
	return nil

}

// NewSearcher returns a new OpenSearchSearcher implementation
func NewSearcher(ctx context.Context, config Config) (port.ResourceSearcher, error) {

	if config.URL == "" {
		slog.ErrorContext(ctx, "opensearch URL is required")
		return nil, fmt.Errorf("opensearch URL is required")
	}
	if config.Index == "" {
		slog.ErrorContext(ctx, "opensearch index is required")
		return nil, fmt.Errorf("opensearch index is required")
	}

	opensearchClient, errpensearchClient := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses: []string{config.URL},
			Transport: otelhttp.NewTransport(&http.Transport{
				MaxIdleConnsPerHost:   10,
				ResponseHeaderTimeout: 30 * time.Second,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			}),
		},
	})
	if errpensearchClient != nil {
		return nil, errors.NewServiceUnavailable("failed to create OpenSearch client", errpensearchClient)
	}
	slog.InfoContext(ctx, "created OpenSearch client created successfully",
		"url", config.URL,
		"index", config.Index,
	)

	return &OpenSearchSearcher{
		client: &httpClient{
			baseURL: config.URL,
			client:  opensearchClient,
		},
		index: config.Index,
	}, nil
}
