// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import "encoding/json"

// Config represents OpenSearch configuration
type Config struct {
	URL   string `json:"url"`
	Index string `json:"index"`
}

// SearchResponse represents the OpenSearch search response
type SearchResponse struct {
	Hits      `json:"hits"`
	PageToken *string `json:"last_item_id,omitempty"`
}

type CountResponse struct {
	Count int `json:"count"`
}

// AggregationBucket represents a single aggregation bucket.
type AggregationBucket struct {
	Key      string `json:"key"`
	DocCount uint64 `json:"doc_count"`
}

// TermsAggregation represents a terms aggregation response.
type TermsAggregation struct {
	DocCountErrorUpperBound uint64              `json:"doc_count_error_upper_bound"`
	SumOtherDocCount        uint64              `json:"sum_other_doc_count"`
	Buckets                 []AggregationBucket `json:"buckets"`
}

// CompositeBucket represents a single composite aggregation bucket. Key maps
// each composite source name to its value.
type CompositeBucket struct {
	Key      map[string]string `json:"key"`
	DocCount uint64            `json:"doc_count"`
}

// CompositeAggregation represents a composite aggregation response.
// AfterKey is absent on the last page.
type CompositeAggregation struct {
	AfterKey map[string]string `json:"after_key,omitempty"`
	Buckets  []CompositeBucket `json:"buckets"`
}

// CountAggregationResponse represents the aggregations a count request may
// ask for. Only the members present in the request are populated.
type CountAggregationResponse struct {
	// AccessKeys is the composite walk over the access-check field.
	AccessKeys *CompositeAggregation `json:"access_keys,omitempty"`
	// GroupBy is the prefix-restricted terms aggregation over tags.
	GroupBy *TermsAggregation `json:"group_by,omitempty"`
	// Tags is the composite walk over tags used for the cardinality metric.
	Tags *CompositeAggregation `json:"tags,omitempty"`
}

// FieldMapping is the subset of an OpenSearch field mapping the searcher
// inspects: the field type and its multi-field subfields.
type FieldMapping struct {
	Type   string                  `json:"type"`
	Fields map[string]FieldMapping `json:"fields,omitempty"`
}

// IndexMapping is the subset of a GET /<index>/_mapping response the
// searcher inspects: the top-level properties of the single index.
type IndexMapping struct {
	Properties map[string]FieldMapping `json:"properties"`
}

// Hits represents the hits in the search response
type Hits struct {
	Total `json:"total"`
	Hits  []Hit `json:"hits"`
}

// Total represents the total number of hits
type Total struct {
	Value int `json:"value"`
}

// Hit represents a single search result hit
type Hit struct {
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}
