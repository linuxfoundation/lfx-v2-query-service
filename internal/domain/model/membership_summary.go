// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

// MembershipSummaryCriteria selects the membership records a summary read
// covers. At least one of the two UIDs is set; both together restrict the read
// to the memberships of that organization on that project.
type MembershipSummaryCriteria struct {
	// ProjectUID is the project whose memberships are summarized.
	ProjectUID string
	// B2BOrgUID is the organization whose memberships are summarized.
	B2BOrgUID string
}

// MembershipTerm is one membership record of an organization on a project, as
// stored on the record.
type MembershipTerm struct {
	// MembershipUID is the UID of the membership record.
	MembershipUID string
	// Status is the membership status stored on the record.
	Status string
	// TierName is the tier name stored on the record.
	TierName string
	// TierRange is the tier range stored on the record; empty when the record
	// carries none.
	TierRange string
	// StartDate is the start date stored on the record; empty when the record
	// carries none.
	StartDate string
	// EndDate is the end date stored on the record; empty when the record
	// carries none.
	EndDate string
}

// MembershipTermSummary is the membership history of one organization on one
// project, folded from the membership records.
type MembershipTermSummary struct {
	// B2BOrgUID is the organization UID carried by the records; empty when
	// they carry none.
	B2BOrgUID string
	// CompanyName is the organization name of the current record.
	CompanyName string
	// ProjectUID is the project UID carried by the records; empty when they
	// carry none.
	ProjectUID string
	// ProjectSlug is the project slug of the current record.
	ProjectSlug string
	// TermCount is the number of membership records folded into this summary.
	TermCount uint64
	// FirstStart is the earliest start date across the records; empty when no
	// record carries one.
	FirstStart string
	// LastEnd is the latest end date across the records; empty when no record
	// carries one.
	LastEnd string
	// CurrentStatus is the status of the current record; empty when there is
	// no current record.
	CurrentStatus string
	// CurrentTierName is the tier name of the current record; empty when there
	// is no current record.
	CurrentTierName string
	// CurrentStart is the start date of the current record; empty when there
	// is no current record.
	CurrentStart string
	// CurrentEnd is the end date of the current record; empty when there is no
	// current record.
	CurrentEnd string
	// CurrentMembershipUID is the UID of the current record; empty when there
	// is no current record.
	CurrentMembershipUID string
	// TierNames holds the distinct tier names in first appearance order.
	TierNames []string
	// Statuses holds the distinct statuses in first appearance order.
	Statuses []string
	// Terms holds the membership records of this organization on this project,
	// oldest first.
	Terms []MembershipTerm
}

// MembershipSummaryResult is the outcome of a membership term summary read.
type MembershipSummaryResult struct {
	// Summaries holds one summary per organization and project.
	Summaries []MembershipTermSummary
	// TermsTotal is the number of membership records folded into the summaries.
	TermsTotal uint64
	// Complete is true when every matching membership record was read, false
	// when the read stopped at the record cap.
	Complete bool
	// CacheControl is the cache-control header value of the response.
	CacheControl string
}
