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
	// MaxCountAccessBuckets is the maximum configurable access-key walk cap.
	// Whole-page overshoot remains below OpenSearch's default max_terms_count.
	MaxCountAccessBuckets = 10000
	// MaxCountAccessPages bounds the configured number of count-walk pages.
	MaxCountAccessPages = 100
	// DefaultMaxSummaryRecords is the default cap on membership records read
	// before a membership summary stops and reports itself incomplete
	DefaultMaxSummaryRecords = 5000
	// MaxSummaryRecordCap is the maximum configurable membership record cap.
	// A whole page is always read, so the cap may be overshot by up to
	// MaxPageSize records.
	MaxSummaryRecordCap = 50000
)

// Membership summary scope: the indexed resource type the read covers and the
// tag prefixes it scopes the read with.
const (
	// MembershipResourceType is the indexed type of a membership record
	MembershipResourceType = "project_membership"
	// MembershipProjectTagPrefix prefixes the tag carrying a membership record's project UID
	MembershipProjectTagPrefix = "project_uid:"
	// MembershipOrgTagPrefix prefixes the tag carrying a membership record's organization UID
	MembershipOrgTagPrefix = "b2b_org_uid:"
)
