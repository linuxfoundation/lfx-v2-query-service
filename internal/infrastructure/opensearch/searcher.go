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
	"golang.org/x/sync/singleflight"
)

// templateFuncs are the helpers available to every query template.
var templateFuncs = template.FuncMap{
	"quote": jsonQuote,
}

// jsonQuote renders s as a JSON string literal. It returns strconv.Quote's
// output whenever that is already valid JSON — which it is for every
// printable string, so the request bodies pinned in tests are unchanged — and
// falls back to encoding/json otherwise. strconv.Quote alone is not enough:
// it emits Go escapes (\x01, \v, \U0001F600) that JSON does not accept, and
// the count route echoes indexed data (composite after_keys, granted access
// keys) into request bodies, so a control character in a tag or key would
// otherwise turn into a marshal error and a 500.
func jsonQuote(s string) string {
	quoted := strconv.Quote(s)
	if json.Valid([]byte(quoted)) {
		return quoted
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string cannot fail; keep the compiler honest.
		return `""`
	}
	return string(encoded)
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
	// HasAfter says whether to render it; an empty-string cursor is a valid
	// cursor and must still be sent.
	After    string
	HasAfter bool

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
	// accessKeyFieldRevalidateInterval bounds stale successful resolutions
	// when operators change an alias's backing indices.
	accessKeyFieldRevalidateInterval = 5 * time.Minute
	// groupByShardSizeFactor and groupByShardSizeMax bound the terms
	// aggregation's shard_size: each shard returns min(size*factor, max)
	// candidates to reduce potential group-count underestimation; only a
	// zero returned error bound establishes exact counts. Composite aggregations (the access
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

	// accessKeyField is usable only while its successful resolution is fresh.
	accessKeyField string
	// accessKeyFieldResolvedAt is the completion time of its last successful read.
	accessKeyFieldResolvedAt time.Time
	// accessKeyFieldRetryAt is the earliest time a failed mapping read is
	// retried; zero means "retry now".
	accessKeyFieldRetryAt time.Time
	accessKeyFieldMu      sync.Mutex
	accessKeyFieldFlight  singleflight.Group
	// now is the clock used for the retry window; tests may override it.
	now func() time.Time
}

// OpenSearchClientRetriever defines the interface for OpenSearch operations
// This allows for easy mocking and testing
type OpenSearchClientRetriever interface {
	Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error)
	Count(ctx context.Context, index string, query []byte) (*CountResponse, error)
	AggregationSearch(ctx context.Context, index string, query []byte) (json.RawMessage, error)
	GetMapping(ctx context.Context, index string) (IndexMappings, error)
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
	slog.DebugContext(ctx, "public resource count query")

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
	accessKeyField, err := os.resolveAccessKeyField(ctx)
	if err != nil {
		return nil, err
	}
	params := countAggregationParams{
		Criteria:       criteria,
		AccessKeyField: accessKeyField,
		AccessWalk:     true,
		PageSize:       request.PageSize,
	}
	if request.After != nil {
		params.After = *request.After
		params.HasAfter = true
	}
	query, err := os.RenderCountAggregation(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "unrecoverable request parsing error", "error", err)
		return nil, fmt.Errorf("failed to render query: %w", err)
	}
	slog.DebugContext(ctx, "access bucket walk query", "page_size", request.PageSize)

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
		AuthorizedFilter: true,
		IncludePublic:    aggregation.IncludePublic,
		AuthorizedKeys:   aggregation.AuthorizedKeys,
	}
	// The access field is only needed to render the granted-keys terms
	// clause. Anonymous callers (public only) never touch it, so a mapping
	// read that is failing cannot affect their answers.
	if len(aggregation.AuthorizedKeys) > 0 {
		accessKeyField, err := os.resolveAccessKeyField(ctx)
		if err != nil {
			return nil, err
		}
		base.AccessKeyField = accessKeyField
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
		slog.DebugContext(ctx, "grouped count query", "prefix", aggregation.GroupByPrefix,
			"size", aggregation.GroupBySize, "authorized_key_count", len(aggregation.AuthorizedKeys))

		response, err := os.aggregationSearch(ctx, query)
		if err != nil {
			return nil, err
		}
		if response.GroupBy == nil {
			return nil, fmt.Errorf("opensearch response is missing the group_by aggregation")
		}
		if response.GroupBy.DocCountErrorUpperBound > 0 {
			// Reported when a shard's candidate list was cut at shard_size;
			// the returned counts may then be lower bounds. The bound is also
			// returned to callers independently of group completeness.
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
		result.GroupCountErrorUpperBound = response.GroupBy.DocCountErrorUpperBound
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
// starting just after the bare "<prefix>:" string (deliberately excluding an
// empty suffix, which is not a value) and stopping at the first key outside the
// prefix, at a short page, or at aggregation.MaxDistinct.
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
		params.HasAfter = true
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
			// A full page that cannot be continued: the value is a lower
			// bound, so say so rather than claim completeness (same rule as
			// the access-bucket walk).
			slog.WarnContext(ctx, "cardinality page was full but carried no cursor; reporting the metric incomplete",
				"prefix", aggregation.CardinalityPrefix,
				"page", page,
				"distinct", distinct,
			)
			return distinct, false, nil
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

// tagPrefixInclude builds the Lucene expression for non-empty values of one
// tag prefix. The API restricts prefixes to [a-z][a-z0-9_]*; quote Lucene
// operators defensively for internal callers as well.
func tagPrefixInclude(prefix string) string {
	return luceneQuoteMeta(prefix) + ":.+"
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
//   - an unsupported shape, disagreeing alias targets, or a failed read
//     -> ServiceUnavailable, warning and retry after accessKeyFieldRetryInterval
//
// Only agreement across every supported backing index is cached, and successful
// resolutions are revalidated every five minutes. Never reuse an expired field
// after failed revalidation: an unmapped subfield can return zero buckets with HTTP 200.
// Anonymous public counts do not need the field and are unaffected. The read
// runs outside the lock so concurrent requests are not serialized behind it.
//
// It also warns when tags is not keyword or data is not flat_object, since
// the grouped count and the cardinality metric depend on both.
func (os *OpenSearchSearcher) resolveAccessKeyField(ctx context.Context) (string, error) {
	// Coalesce startup and retry-boundary callers. The cache is rechecked
	// inside the flight, so a late caller cannot start another mapping read.
	value, err, _ := os.accessKeyFieldFlight.Do("mapping", func() (any, error) {
		return os.readAccessKeyField(ctx)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

// readAccessKeyField runs only inside a mapping flight; successful results
// and failed-read retry windows are shared by every caller.
func (os *OpenSearchSearcher) readAccessKeyField(ctx context.Context) (string, error) {
	now := time.Now
	if os.now != nil {
		now = os.now
	}

	os.accessKeyFieldMu.Lock()
	if os.accessKeyField != "" && !os.accessKeyFieldResolvedAt.IsZero() && now().Before(os.accessKeyFieldResolvedAt.Add(accessKeyFieldRevalidateInterval)) {
		field := os.accessKeyField
		os.accessKeyFieldMu.Unlock()
		return field, nil
	}
	if !os.accessKeyFieldRetryAt.IsZero() && now().Before(os.accessKeyFieldRetryAt) {
		os.accessKeyFieldMu.Unlock()
		return "", errors.NewServiceUnavailable("index mapping unavailable; access-checked counts cannot be answered until the next mapping read")
	}
	previousField := os.accessKeyField
	// Discard the expired success before I/O. Every failure path below must
	// leave it unavailable instead of falling back to stale alias mappings.
	os.accessKeyField = ""
	os.accessKeyFieldResolvedAt = time.Time{}
	os.accessKeyFieldMu.Unlock()

	// The outcome is process-wide, so the read must not inherit the
	// caller's cancellation or deadline: a client that hangs up mid-read
	// would otherwise open the retry window for everyone. Trace values are
	// kept; only cancellation is dropped.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accessKeyFieldReadTimeout)
	mappings, err := os.client.GetMapping(readCtx, os.index)
	cancel()
	if err == nil && len(mappings) == 0 {
		err = fmt.Errorf("opensearch get mapping returned no mapping")
	}

	os.accessKeyFieldMu.Lock()
	defer os.accessKeyFieldMu.Unlock()
	if err != nil {
		os.accessKeyFieldRetryAt = now().Add(accessKeyFieldRetryInterval)
		slog.WarnContext(ctx, "could not read index mapping; access-checked counts fail closed until the next retry",
			"index", os.index,
			"retry_after", accessKeyFieldRetryInterval,
			"error", err,
		)
		return "", errors.NewServiceUnavailable("index mapping unavailable; access-checked counts cannot be answered until the next mapping read", err)
	}

	var commonField string
	for name, mapping := range mappings {
		field, resolved := accessKeyFieldFromMapping(&mapping)
		if !resolved {
			os.accessKeyFieldRetryAt = now().Add(accessKeyFieldRetryInterval)
			slog.WarnContext(ctx, "access_check_query mapping unsupported",
				"index", os.index, "backing_index", name,
				"observed_type", mapping.Properties[accessCheckQueryField].Type)
			return "", errors.NewServiceUnavailable("access_check_query mapping unsupported; configure keyword or text with a keyword subfield and retry")
		}
		if commonField != "" && field != commonField {
			os.accessKeyFieldRetryAt = now().Add(accessKeyFieldRetryInterval)
			slog.WarnContext(ctx, "access_check_query alias mappings disagree or are unsupported",
				"index", os.index, "backing_index", name, "index_count", len(mappings))
			return "", errors.NewServiceUnavailable("index mappings must agree on one supported access_check_query field; repair the alias mappings and retry")
		}
		commonField = field
		if tags, ok := mapping.Properties["tags"]; !ok || tags.Type != "keyword" {
			slog.WarnContext(ctx, "tags is not mapped as keyword; grouped counts and cardinality metrics may not work",
				"index", name, "observed_type", tags.Type)
		}
		if data, ok := mapping.Properties["data"]; !ok || data.Type != "flat_object" {
			slog.WarnContext(ctx, "data is not mapped as flat_object; the count route assumes data fields are not aggregatable",
				"index", name, "observed_type", data.Type)
		}
	}
	// Do not memoize until every backing index has been checked.
	os.accessKeyField = commonField
	os.accessKeyFieldResolvedAt = now()
	os.accessKeyFieldRetryAt = time.Time{}

	if os.accessKeyField != previousField {
		slog.InfoContext(ctx, "resolved access key field",
			"index", os.index,
			"access_key_field", os.accessKeyField,
		)
	}
	return os.accessKeyField, nil
}

// accessKeyFieldFromMapping applies the resolution table of
// resolveAccessKeyField to a mapping. The boolean is false when the mapping
// did not match a supported shape; there is no fallback field.
func accessKeyFieldFromMapping(mapping *IndexMapping) (string, bool) {
	if mapping == nil {
		return "", false
	}
	field, ok := mapping.Properties[accessCheckQueryField]
	if !ok {
		return "", false
	}
	switch field.Type {
	case "keyword":
		return accessCheckQueryField, true
	case "text":
		if sub, ok := field.Fields["keyword"]; ok && sub.Type == "keyword" {
			return accessCheckQueryKeywordField, true
		}
	}
	return "", false
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
		// The body is not logged: it carries caller tags, composite cursors
		// and granted access keys.
		slog.ErrorContext(ctx, "failed to marshal rendered count aggregation", "error", err, "body_len", buf.Len())
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
		Resources:       make([]model.Resource, 0, len(response.Hits.Hits)),
		PageToken:       response.PageToken,
		NextSearchAfter: response.SearchAfter,
		Total:           response.Value,
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
	if len(hit.Sort) > 0 {
		resource.SortValues = string(hit.Sort)
	}

	// Parse the source data. Unmarshal once into TransactionBodyStub plus a
	// raw "data" field, so the (often much larger) "data" payload is only
	// unmarshalled a second time on its own isolated bytes, not re-decoded
	// as part of the whole source.
	if hit.Source != nil {
		var parsed struct {
			model.TransactionBodyStub
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(hit.Source, &parsed); err != nil {
			return resource, fmt.Errorf("failed to unmarshal source data: %w", err)
		}
		resource.TransactionBodyStub = parsed.TransactionBodyStub
		resource.Type = parsed.ObjectType

		if len(parsed.Data) > 0 {
			var data any
			if err := json.Unmarshal(parsed.Data, &data); err != nil {
				return resource, fmt.Errorf("failed to unmarshal data field: %w", err)
			}
			resource.Data = data
		} else {
			// No separate data field: use the entire source as data.
			var data any
			if err := json.Unmarshal(hit.Source, &data); err != nil {
				return resource, fmt.Errorf("failed to unmarshal source data: %w", err)
			}
			resource.Data = data
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
