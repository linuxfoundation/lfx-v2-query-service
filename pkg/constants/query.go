// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

const (

	// DefaultPageSize is the default number of results per page for queries
	DefaultPageSize = 50
	// MaxPageSize is the maximum allowed number of results per page
	MaxPageSize = 1000
	// DefaultBucketSize is the default size of the bucket for queries
	DefaultBucketSize = 100
	// DefaultGroupBySize is the default maximum number of groups returned by a grouped count
	DefaultGroupBySize = 100
	// MaxGroupBySize is the maximum number of groups a caller may request from a grouped count
	MaxGroupBySize = 1000
	// DefaultAccessBucketPage is the default number of access-key buckets fetched per composite page
	DefaultAccessBucketPage = 100
	// MaxAccessBucketPage is the maximum configurable composite page size
	MaxAccessBucketPage = 1000
	// DefaultMaxAccessBuckets is the default cap on access-key buckets walked before a count reports has_more
	DefaultMaxAccessBuckets = 5000
)
