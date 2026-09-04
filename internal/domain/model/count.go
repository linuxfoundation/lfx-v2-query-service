// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

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

// AccessBucketPage is one page of the composite walk over private
// resources grouped by their access-check key.
type AccessBucketPage struct {
	// Buckets holds one entry per distinct access-check key on this page.
	Buckets []AggregationBucket
	// AfterKey is the composite cursor for the next page. It is nil on the
	// last page; callers must still treat a short page as the last one even
	// when OpenSearch returns an after_key on it.
	AfterKey *string
}

// AccessBucketRequest describes one page request of the access-key walk.
type AccessBucketRequest struct {
	// PageSize is the composite aggregation size (buckets per page).
	PageSize int
	// After is the after_key of the previous page; nil for the first page.
	After *string
}

// CountAggregation describes the aggregation to run over the resources the
// caller is authorized to see: an optional grouped count and an optional
// distinct-value metric. At most one of GroupByPrefix and CardinalityPrefix
// is set (the converter rejects both together).
type CountAggregation struct {
	// GroupByPrefix is the tag prefix (without the trailing ':') to group by.
	// Empty means no grouping.
	GroupByPrefix string
	// GroupBySize is the maximum number of groups to return.
	GroupBySize int
	// CardinalityPrefix is the tag prefix (without the trailing ':') whose
	// distinct values are counted. Empty means no metric.
	CardinalityPrefix string
	// IncludePublic adds public resources to the authorized set.
	IncludePublic bool
	// AuthorizedKeys are the access-check keys the principal was granted;
	// private resources carrying one of them are part of the authorized set.
	AuthorizedKeys []string
	// PageSize is the composite page size for the cardinality walk.
	PageSize int
	// MaxDistinct caps the cardinality walk; beyond it MetricComplete is false.
	MaxDistinct int
}

// HasWork reports whether the aggregation asks for anything.
func (a CountAggregation) HasWork() bool {
	return a.GroupByPrefix != "" || a.CardinalityPrefix != ""
}

// CountGroup is one group of a grouped count.
type CountGroup struct {
	// Key is the tag value with the group_by prefix stripped.
	Key string
	// Count is the number of authorized resources carrying that tag.
	Count uint64
}

// CountAggregationResult holds the outcome of a CountAggregation.
type CountAggregationResult struct {
	// Groups is populated when GroupByPrefix was set.
	Groups []CountGroup
	// GroupsComplete is true when every group is present.
	GroupsComplete bool
	// MetricValue is populated when CardinalityPrefix was set.
	MetricValue uint64
	// MetricComplete is true when the distinct-value walk finished.
	MetricComplete bool
}
