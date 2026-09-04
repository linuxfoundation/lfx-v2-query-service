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
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"

	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// templateFuncs are the helpers available to every query template.
var templateFuncs = template.FuncMap{
	"quote": strconv.Quote,
}

// queryResourceTemplate renders /_search and /_count bodies. The shared
// criteria sub-templates are parsed first so both templates embed the same
// definitions.
var queryResourceTemplate = template.Must(
	template.Must(
		template.New("queryResource").Funcs(templateFuncs).Parse(criteriaSource),
	).Parse(queryResourceSource))

// countAggregationTemplate renders the aggregation-only bodies of the count
// route (access-key walk, grouped count, cardinality walk).
var countAggregationTemplate = template.Must(
	template.Must(
		template.New("countAggregation").Funcs(templateFuncs).Parse(criteriaSource),
	).Parse(countAggregationSource))

// countAggregationParams is the data passed to countAggregationTemplate.
type countAggregationParams struct {
	// Criteria carries the caller's search parameters plus the
	// PublicOnly/PrivateOnly switches for the outer bool query.
	Criteria model.SearchCriteria
	// AccessKeyField is the resolved access-check field (see
	// resolveAccessKeyField).
	AccessKeyField string

	// AccessWalk renders the composite aggregation over the access field.
	AccessWalk bool
	// PageSize is the composite page size (access walk and cardinality walk).
	PageSize int
	// After is the composite cursor: the previous page's after_key, or, for
	// the cardinality walk's first page, the bare "<prefix>:" string.
	After string

	// AuthorizedFilter adds the "filter" bool restricting documents to the
	// authorized set; IncludePublic and AuthorizedKeys are its two branches.
	AuthorizedFilter bool
	IncludePublic    bool
	AuthorizedKeys   []string

	// GroupByPrefix renders the prefix-restricted terms aggregation over tags.
	GroupByPrefix  string
	GroupBySize    int
	GroupByInclude string
	// GroupByShardSize is the per-shard candidate count for the terms
	// aggregation (see groupByShardSize).
	GroupByShardSize int

	// CardinalityPrefix renders the composite walk over tags.
	CardinalityPrefix string
}

const (
	// accessCheckQueryField is the indexed field holding
	// "{access_check_object}#{access_check_relation}".
	accessCheckQueryField = "access_check_query"
	// accessCheckQueryKeywordField is the multi-field subfield produced by a
	// dynamic text mapping of accessCheckQueryField.
	accessCheckQueryKeywordField = accessCheckQueryField + ".keyword"
	// accessKeyFieldRetryInterval bounds how often a failed mapping read is
	// retried, so a persistently unreadable mapping (for example a service
	// account without indices:admin/mappings/get) costs one extra round-trip
	// per interval instead of one per request.
	accessKeyFieldRetryInterval = 30 * time.Second
	// groupByShardSizeFactor and groupByShardSizeMax bound the terms
	// aggregation's shard_size: each shard returns min(size*factor, max)
	// candidates so that, on a multi-shard index, the top groups and their
	// doc_counts are exact in practice. Composite aggregations (the access
	// walk and the cardinality walk) are exact by construction and need no
	// such tuning.
	groupByShardSizeFactor = 5
	groupByShardSizeMax    = 5000
	// accessKeyFieldReadTimeout bounds the mapping read itself. The read is
	// decoupled from the caller's context so one cancelled request cannot
	// open the retry window for every other request.
	accessKeyFieldReadTimeout = 5 * time.Second
)

// OpenSearchSearcher implements the ResourceSearcher interface for OpenSearch
type OpenSearchSearcher struct {
	client OpenSearchClientRetriever
	index  string

	// accessKeyField is resolved from the live index mapping on first use
	// (see resolveAccessKeyField). Tests may preset it to skip resolution.
	accessKeyField string
	// accessKeyFieldRetryAt is the earliest time a failed mapping read is
	// retried; zero means "retry now".
	accessKeyFieldRetryAt time.Time
	accessKeyFieldMu      sync.Mutex
	// now is the clock used for the retry window; tests may override it.
	now func() time.Time
}

// OpenSearchClientRetriever defines the interface for OpenSearch operations
// This allows for easy mocking and testing
type OpenSearchClientRetriever interface {
	Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error)
	Count(ctx context.Context, index string, query []byte) (*CountResponse, error)
	AggregationSearch(ctx context.Context, index string, query []byte) (json.RawMessage, error)
	GetMapping(ctx context.Context, index string) (*IndexMapping, error)
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

// CountPublic implements port.ResourceSearcher: a /_count over the public
// resources matching the criteria.
func (os *OpenSearchSearcher) CountPublic(ctx context.Context, criteria model.SearchCriteria) (int, error) {
	if !criteria.PublicOnly {
		// Not expected: the converter always builds the public criteria with PublicOnly.
		return 0, fmt.Errorf("CountPublic requires PublicOnly criteria")
	}
	query, err := os.Render(ctx, criteria)
	if err != nil {
		// Not expected to happen: this is an error with our interpolation logic.
		slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
		return 0, fmt.Errorf("failed to render query: %w", err)
	}
	slog.DebugContext(ctx, "public resource count query", "query", string(query))

	countResponse, err := os.client.Count(ctx, os.index, query)
	if err != nil {
		return 0, fmt.Errorf("opensearch count failed: %w", err)
	}
	return countResponse.Count, nil
}

// AccessBuckets implements port.ResourceSearcher: one composite page of the
// private resources matching the criteria, grouped by access-check key.
func (os *OpenSearchSearcher) AccessBuckets(ctx context.Context, criteria model.SearchCriteria, request model.AccessBucketRequest) (*model.AccessBucketPage, error) {
	if !criteria.PrivateOnly {
		// Not expected: the converter always builds the walk criteria with PrivateOnly.
		return nil, fmt.Errorf("AccessBuckets requires PrivateOnly criteria")
	}
	params := countAggregationParams{
		Criteria:       criteria,
		AccessKeyField: os.resolveAccessKeyField(ctx),
		AccessWalk:     true,
		PageSize:       request.PageSize,
	}
	if request.After != nil {
		params.After = *request.After
	}
	query, err := os.RenderCountAggregation(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
		return nil, fmt.Errorf("failed to render query: %w", err)
	}
	slog.DebugContext(ctx, "access bucket walk query", "query", string(query))

	response, err := os.aggregationSearch(ctx, query)
	if err != nil {
		return nil, err
	}
	if response.AccessKeys == nil {
		return nil, fmt.Errorf("opensearch response is missing the access_keys aggregation")
	}

	page := &model.AccessBucketPage{
		Buckets: make([]model.AggregationBucket, 0, len(response.AccessKeys.Buckets)),
	}
	for _, bucket := range response.AccessKeys.Buckets {
		page.Buckets = append(page.Buckets, model.AggregationBucket{
			Key:      bucket.Key["access_key"],
			DocCount: bucket.DocCount,
		})
	}
	if after, ok := response.AccessKeys.AfterKey["access_key"]; ok {
		page.AfterKey = &after
	}
	return page, nil
}

// AuthorizedAggregation implements port.ResourceSearcher: the grouped count
// and/or the distinct tag-value metric over the authorized resources.
func (os *OpenSearchSearcher) AuthorizedAggregation(ctx context.Context, criteria model.SearchCriteria, aggregation model.CountAggregation) (*model.CountAggregationResult, error) {
	result := &model.CountAggregationResult{
		Groups:         []model.CountGroup{},
		GroupsComplete: true,
		MetricComplete: true,
	}
	if !aggregation.HasWork() {
		return result, nil
	}
	if !aggregation.IncludePublic && len(aggregation.AuthorizedKeys) == 0 {
		// Nothing is visible to the caller: no query needed.
		return result, nil
	}

	base := countAggregationParams{
		Criteria:         criteria,
		AccessKeyField:   os.resolveAccessKeyField(ctx),
		AuthorizedFilter: true,
		IncludePublic:    aggregation.IncludePublic,
		AuthorizedKeys:   aggregation.AuthorizedKeys,
	}

	if aggregation.GroupByPrefix != "" {
		params := base
		params.GroupByPrefix = aggregation.GroupByPrefix
		params.GroupBySize = aggregation.GroupBySize
		params.GroupByShardSize = groupByShardSize(aggregation.GroupBySize)
		params.GroupByInclude = tagPrefixInclude(aggregation.GroupByPrefix)
		query, err := os.RenderCountAggregation(ctx, params)
		if err != nil {
			slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
			return nil, fmt.Errorf("failed to render query: %w", err)
		}
		slog.DebugContext(ctx, "grouped count query", "query", string(query))

		response, err := os.aggregationSearch(ctx, query)
		if err != nil {
			return nil, err
		}
		if response.GroupBy == nil {
			return nil, fmt.Errorf("opensearch response is missing the group_by aggregation")
		}
		if response.GroupBy.DocCountErrorUpperBound > 0 {
			// Only possible on a multi-shard index when a shard's candidate
			// list was cut at shard_size; the reported counts may then be
			// lower bounds. Not surfaced to callers.
			slog.DebugContext(ctx, "grouped count has a non-zero doc_count_error_upper_bound",
				"prefix", aggregation.GroupByPrefix,
				"size", aggregation.GroupBySize,
				"shard_size", params.GroupByShardSize,
				"doc_count_error_upper_bound", response.GroupBy.DocCountErrorUpperBound,
			)
		}
		prefix := aggregation.GroupByPrefix + ":"
		result.Groups = make([]model.CountGroup, 0, len(response.GroupBy.Buckets))
		for _, bucket := range response.GroupBy.Buckets {
			result.Groups = append(result.Groups, model.CountGroup{
				Key:   strings.TrimPrefix(bucket.Key, prefix),
				Count: bucket.DocCount,
			})
		}
		result.GroupsComplete = response.GroupBy.SumOtherDocCount == 0
	}

	if aggregation.CardinalityPrefix != "" {
		value, complete, err := os.walkCardinality(ctx, base, aggregation)
		if err != nil {
			return nil, err
		}
		result.MetricValue = value
		result.MetricComplete = complete
	}

	return result, nil
}

// walkCardinality counts the distinct "<prefix>:…" tags in the authorized set
// by paging a composite aggregation over tags in ascending key order,
// starting just after the bare "<prefix>:" string and stopping at the first
// key outside the prefix, at a short page, or at aggregation.MaxDistinct.
func (os *OpenSearchSearcher) walkCardinality(ctx context.Context, base countAggregationParams, aggregation model.CountAggregation) (uint64, bool, error) {
	prefix := aggregation.CardinalityPrefix + ":"
	pageSize := aggregation.PageSize
	if pageSize <= 0 {
		pageSize = constants.DefaultAccessBucketPage
	}
	maxDistinct := aggregation.MaxDistinct
	if maxDistinct <= 0 {
		maxDistinct = constants.DefaultMaxAccessBuckets
	}

	var distinct uint64
	after := prefix
	for page := 1; ; page++ {
		params := base
		params.CardinalityPrefix = aggregation.CardinalityPrefix
		params.PageSize = pageSize
		params.After = after
		query, err := os.RenderCountAggregation(ctx, params)
		if err != nil {
			slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
			return 0, false, fmt.Errorf("failed to render query: %w", err)
		}
		// The rendered body is not logged: its "after" cursor carries a tag
		// value (for metric=cardinality:email, an address).
		slog.DebugContext(ctx, "cardinality walk page",
			"prefix", aggregation.CardinalityPrefix,
			"page", page,
			"size", pageSize,
		)

		response, err := os.aggregationSearch(ctx, query)
		if err != nil {
			return 0, false, err
		}
		if response.Tags == nil {
			return 0, false, fmt.Errorf("opensearch response is missing the tags aggregation")
		}

		for _, bucket := range response.Tags.Buckets {
			key := bucket.Key["tag"]
			if !strings.HasPrefix(key, prefix) {
				// Composite terms sources page in ascending key order, so the
				// first key outside the prefix ends the prefix range.
				return distinct, true, nil
			}
			distinct++
			if distinct >= uint64(maxDistinct) {
				return distinct, false, nil
			}
		}
		if len(response.Tags.Buckets) < pageSize {
			return distinct, true, nil
		}
		next, ok := response.Tags.AfterKey["tag"]
		if !ok {
			return distinct, true, nil
		}
		after = next
	}
}

// aggregationSearch runs an aggregation body and decodes the count-route
// aggregation shape.
func (os *OpenSearchSearcher) aggregationSearch(ctx context.Context, query []byte) (*CountAggregationResponse, error) {
	raw, err := os.client.AggregationSearch(ctx, os.index, query)
	if err != nil {
		return nil, fmt.Errorf("opensearch search failed: %w", err)
	}
	var response CountAggregationResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &response); err != nil {
			slog.ErrorContext(ctx, "failed to unmarshal aggregations", "error", err)
			return nil, fmt.Errorf("unrecoverable aggregation processing error: %w", err)
		}
	}
	return &response, nil
}

// groupByShardSize returns min(size*groupByShardSizeFactor, groupByShardSizeMax).
func groupByShardSize(size int) int {
	shardSize := size * groupByShardSizeFactor
	if shardSize > groupByShardSizeMax {
		return groupByShardSizeMax
	}
	return shardSize
}

// tagPrefixInclude builds the Lucene regular expression that restricts a
// terms aggregation over tags to one prefix. The prefix is validated by the
// API to [a-z][a-z0-9_]*, so no character needs escaping; regexp.QuoteMeta
// is still applied defensively (its escapes are also Lucene escapes for
// these characters).
func tagPrefixInclude(prefix string) string {
	return luceneQuoteMeta(prefix) + ":.*"
}

// luceneQuoteMeta escapes the Lucene regular-expression operators. Go's
// regexp.QuoteMeta is not used because it also escapes characters Lucene
// treats literally.
func luceneQuoteMeta(s string) string {
	const operators = `.?+*|{}[]()"\#@&<>~`
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(operators, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// resolveAccessKeyField decides which indexed field the access-key walk
// aggregates on, by reading the live index mapping:
//
//   - access_check_query mapped as keyword          -> "access_check_query"
//   - text with a keyword subfield                  -> "access_check_query.keyword"
//   - anything else                                 -> "access_check_query.keyword"
//     (the pre-existing behaviour), with a warning.
//
// A successful read is memoized for the life of the process. A failed
// mapping call is not: the default is used with a warning and the read is
// retried after accessKeyFieldRetryInterval, so a transient OpenSearch error
// at boot cannot pin the wrong field (and the silent zero buckets it
// produces on a plain-keyword index) until a restart, while a persistent
// failure does not put a network call on every request. The mapping call
// itself runs outside the lock so concurrent requests are not serialized
// behind it.
//
// It also warns when tags is not keyword or data is not flat_object, since
// the grouped count and the cardinality metric depend on both.
func (os *OpenSearchSearcher) resolveAccessKeyField(ctx context.Context) string {
	now := time.Now
	if os.now != nil {
		now = os.now
	}

	os.accessKeyFieldMu.Lock()
	if os.accessKeyField != "" {
		field := os.accessKeyField
		os.accessKeyFieldMu.Unlock()
		return field
	}
	if !os.accessKeyFieldRetryAt.IsZero() && now().Before(os.accessKeyFieldRetryAt) {
		os.accessKeyFieldMu.Unlock()
		return accessCheckQueryKeywordField
	}
	os.accessKeyFieldMu.Unlock()

	// The outcome is process-wide, so the read must not inherit the
	// caller's cancellation or deadline: a client that hangs up mid-read
	// would otherwise open the retry window for everyone. Trace values are
	// kept; only cancellation is dropped.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accessKeyFieldReadTimeout)
	mapping, err := os.client.GetMapping(readCtx, os.index)
	cancel()

	os.accessKeyFieldMu.Lock()
	defer os.accessKeyFieldMu.Unlock()
	if os.accessKeyField != "" {
		// Another request resolved it while this one was on the wire.
		return os.accessKeyField
	}
	if err != nil {
		os.accessKeyFieldRetryAt = now().Add(accessKeyFieldRetryInterval)
		slog.WarnContext(ctx, "could not read index mapping; using default access key field until the next retry",
			"index", os.index,
			"access_key_field", accessCheckQueryKeywordField,
			"retry_after", accessKeyFieldRetryInterval,
			"error", err,
		)
		return accessCheckQueryKeywordField
	}

	field, resolved := accessKeyFieldFromMapping(mapping)
	if !resolved {
		observed, _ := json.Marshal(mapping.Properties[accessCheckQueryField])
		slog.WarnContext(ctx, "unexpected access_check_query mapping; using default access key field",
			"index", os.index,
			"access_key_field", field,
			"observed_mapping", string(observed),
		)
	}
	os.accessKeyField = field
	os.accessKeyFieldRetryAt = time.Time{}

	if tags, ok := mapping.Properties["tags"]; !ok || tags.Type != "keyword" {
		slog.WarnContext(ctx, "tags is not mapped as keyword; grouped counts and cardinality metrics may not work",
			"index", os.index,
			"observed_type", tags.Type,
		)
	}
	if data, ok := mapping.Properties["data"]; !ok || data.Type != "flat_object" {
		slog.WarnContext(ctx, "data is not mapped as flat_object; the count route assumes data fields are not aggregatable",
			"index", os.index,
			"observed_type", data.Type,
		)
	}

	slog.InfoContext(ctx, "resolved access key field",
		"index", os.index,
		"access_key_field", os.accessKeyField,
	)
	return os.accessKeyField
}

// accessKeyFieldFromMapping applies the resolution table of
// resolveAccessKeyField to a mapping. The boolean is false when the mapping
// did not match a known shape and the default is being returned.
func accessKeyFieldFromMapping(mapping *IndexMapping) (string, bool) {
	if mapping == nil {
		return accessCheckQueryKeywordField, false
	}
	field, ok := mapping.Properties[accessCheckQueryField]
	if !ok {
		return accessCheckQueryKeywordField, false
	}
	switch field.Type {
	case "keyword":
		return accessCheckQueryField, true
	case "text":
		if sub, ok := field.Fields["keyword"]; ok && sub.Type == "keyword" {
			return accessCheckQueryKeywordField, true
		}
	}
	return accessCheckQueryKeywordField, false
}

// RenderCountAggregation generates an aggregation-only body for the count route.
func (os *OpenSearchSearcher) RenderCountAggregation(ctx context.Context, params countAggregationParams) ([]byte, error) {
	var buf bytes.Buffer
	if err := countAggregationTemplate.Execute(&buf, params); err != nil {
		slog.ErrorContext(ctx, "failed to render count aggregation template", "error", err)
		return nil, err
	}
	query := json.RawMessage(buf.Bytes())

	parsed, err := json.Marshal(query)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal rendered count aggregation", "error", err, "body", buf.String())
		return nil, err
	}
	return parsed, nil
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
			Transport: otelhttp.NewTransport(
				&http.Transport{
					MaxIdleConnsPerHost:   10,
					ResponseHeaderTimeout: 30 * time.Second,
					DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				},
				// peer.service was removed from OTel semconv in v1.39 (replaced by
				// resource-level service.name). It is set here as a raw attribute
				// because Datadog still uses it to label downstream nodes in the
				// service map; without it, Datadog falls back to server.address
				// which resolves to the raw AWS VPC hostname.
				otelhttp.WithSpanOptions(trace.WithAttributes(
					attribute.String("peer.service", "opensearch"),
					semconv.DBSystemNameOpenSearch,
				)),
			),
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
