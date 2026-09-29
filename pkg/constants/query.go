// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

import "time"

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
	// DefaultDeniedPageWalk is the default number of additional raw OpenSearch
	// pages QueryResources fetches when a page leaves the caller no visible
	// resource (after cel_filter and the access check), before it gives up and
	// returns the page as-is.
	DefaultDeniedPageWalk = 10
	// MaxDeniedPageWalk is the maximum configurable denied-page walk. Each extra
	// page is a sequential OpenSearch query plus an access-check batch bounded
	// by ACCESS_CHECK_TIMEOUT, so the ceiling stays small.
	MaxDeniedPageWalk = 25
	// MaxCountAccessPages bounds the configured number of count-walk pages.
	MaxCountAccessPages = 100
	// DefaultMaxSummaryRecords is the default cap on membership records read
	// before the read stops at the next organization boundary and reports
	// complete: false
	DefaultMaxSummaryRecords = 5000
	// MaxSummaryRecordCap is the maximum configurable membership record cap.
	// Whole-page reads and completion of the first organization run can
	// overshoot the configured cap, bounded by MaxSummaryRunRecords.
	MaxSummaryRecordCap = 50000
	// MaxSummaryRunRecords bounds a summary read with no resumable run
	// boundary. Reuse the startup-validated cap ceiling, without a new knob:
	// if the run's end cannot be established within this many raw hits,
	// fail the read rather than return a partial summary.
	MaxSummaryRunRecords = MaxSummaryRecordCap
	// MembershipSummaryTokenVersion identifies the whole-run summary read.
	// Tokens from the unversioned read may point inside a run and must not
	// be accepted by this version. This does not version other query tokens.
	MembershipSummaryTokenVersion = 1
	// DefaultCountRequestTimeout bounds the total wall-clock time
	// QueryResourcesCount may spend across every round-trip pair (search
	// plus access check) it issues to satisfy one request. Individual
	// calls have their own timeouts, but nothing else bounds the sum.
	DefaultCountRequestTimeout = 30 * time.Second
	// MaxCountRequestTimeout is the maximum configurable count-request deadline.
	MaxCountRequestTimeout = 5 * time.Minute
	// DefaultSearchRequestTimeout bounds the total wall-clock time
	// QueryResources may spend across every round-trip it issues to satisfy
	// one request: the denied-page walk's raw OpenSearch queries plus each
	// page's batched (possibly chunked and retried) access check.
	// Individual calls have their own timeouts, but nothing else bounds the
	// sum, so a page whose access checks are chunked into many small NATS
	// round trips (a small ACCESS_CHECK_CHUNK_BYTES) or that retries
	// repeatedly (ACCESS_CHECK_RETRIES) could otherwise run for a very long
	// time; this deadline fails the whole request fast instead.
	DefaultSearchRequestTimeout = 30 * time.Second
	// MaxSearchRequestTimeout is the maximum configurable search-request deadline.
	MaxSearchRequestTimeout = 5 * time.Minute
	// DefaultSummaryRequestTimeout bounds the total wall-clock time
	// QueryMembershipSummary may spend across every round-trip it issues:
	// each page's raw OpenSearch query plus its batched (possibly chunked
	// and retried) access check. Individual calls have their own timeouts,
	// but nothing else bounds the sum, so a scope with many pages or a page
	// whose access checks are chunked into many small NATS round trips
	// could otherwise run for a very long time; this deadline fails the
	// whole request fast instead.
	DefaultSummaryRequestTimeout = 30 * time.Second
	// MaxSummaryRequestTimeout is the maximum configurable summary-request deadline.
	MaxSummaryRequestTimeout = 5 * time.Minute
	// DefaultAccessCheckChunkBytes is the default soft ceiling on the size of
	// a single batched access-check message sent to fga-sync over NATS. A
	// message built from a large result page is split into chunks no bigger
	// than this before it is sent, so it stays comfortably under NATS'
	// default 1MiB max payload regardless of how many resources a page held.
	DefaultAccessCheckChunkBytes = 512 * 1024
	// MaxAccessCheckChunkBytes is the maximum configurable access-check chunk size.
	MaxAccessCheckChunkBytes = 1024 * 1024
	// AccessCheckNATSHeaderMargin reserves headroom below NATS' default 1MiB
	// max_payload for the OpenTelemetry trace-context headers
	// requestWithSpan attaches to every outbound access-check publish:
	// max_payload bounds the HPUB header block plus data together, not the
	// data alone, so a chunk sized right up against MaxAccessCheckChunkBytes
	// could still be rejected as oversized once its headers are counted.
	AccessCheckNATSHeaderMargin = 8 * 1024
	// DefaultAccessCheckRetries is the default number of retries for a single
	// access-check chunk that fails outright (e.g. a transient NATS timeout),
	// on top of the initial attempt.
	DefaultAccessCheckRetries = 1
	// MaxAccessCheckRetries is the maximum configurable access-check retry count.
	MaxAccessCheckRetries = 5
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
