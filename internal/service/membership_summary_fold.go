// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"sort"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// Field names of a membership record as stored in the indexed document data.
const (
	membershipFieldUID         = "uid"
	membershipFieldOrgUID      = "b2b_org_uid"
	membershipFieldCompanyName = "company_name"
	membershipFieldProjectUID  = "project_uid"
	membershipFieldProjectSlug = "project_slug"
	membershipFieldStatus      = "status"
	membershipFieldTierName    = "tier_name"
	membershipFieldTierRange   = "tier"
	membershipFieldStartDate   = "start_date"
	membershipFieldEndDate     = "end_date"
	membershipFieldCreatedAt   = "created_at"
)

// membershipStatusActive is the status a record carries while it runs.
const membershipStatusActive = "Active"

// membershipRow is one membership record reduced to the fields the fold reads.
// Dates and UIDs are kept as stored and compared as opaque strings.
type membershipRow struct {
	uid         string
	orgUID      string
	companyName string
	projectUID  string
	projectSlug string
	status      string
	tierName    string
	tierRange   string
	startDate   string
	endDate     string
	createdAt   string
}

// membershipGroupKey identifies the organization and project a record folds
// under. Each half falls back to the record's label when the UID is missing,
// so a record is never dropped for want of an identifier.
type membershipGroupKey struct {
	org     string
	project string
}

// foldMembershipTerms folds membership records into one summary per
// organization and project. Every row is folded: rows carrying neither an
// organization UID nor a company name, or neither a project UID nor a slug,
// fold under the empty value. Summaries come back ordered by organization
// name, project slug, organization UID and project UID.
func foldMembershipTerms(rows []map[string]any) []model.MembershipTermSummary {
	groups := make(map[membershipGroupKey][]membershipRow, len(rows))
	order := make([]membershipGroupKey, 0, len(rows))

	for _, raw := range rows {
		row := newMembershipRow(raw)
		key := row.groupKey()
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}

	summaries := make([]model.MembershipTermSummary, 0, len(order))
	for _, key := range order {
		summaries = append(summaries, summarizeMembershipRows(groups[key]))
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		left, right := summaries[i], summaries[j]
		switch {
		case left.CompanyName != right.CompanyName:
			return left.CompanyName < right.CompanyName
		case left.ProjectSlug != right.ProjectSlug:
			return left.ProjectSlug < right.ProjectSlug
		case left.B2BOrgUID != right.B2BOrgUID:
			return left.B2BOrgUID < right.B2BOrgUID
		default:
			return left.ProjectUID < right.ProjectUID
		}
	})

	return summaries
}

// summarizeMembershipRows folds the records of one organization and project.
func summarizeMembershipRows(rows []membershipRow) model.MembershipTermSummary {
	sorted := make([]membershipRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].before(sorted[j]) })

	summary := model.MembershipTermSummary{
		TermCount: uint64(len(sorted)),
		TierNames: make([]string, 0, len(sorted)),
		Statuses:  make([]string, 0, len(sorted)),
		Terms:     make([]model.MembershipTerm, 0, len(sorted)),
	}

	seenTierNames := make(map[string]struct{}, len(sorted))
	seenStatuses := make(map[string]struct{}, len(sorted))

	for _, row := range sorted {
		summary.Terms = append(summary.Terms, row.term())

		if row.startDate != "" && (summary.FirstStart == "" || row.startDate < summary.FirstStart) {
			summary.FirstStart = row.startDate
		}
		if row.endDate != "" && row.endDate > summary.LastEnd {
			summary.LastEnd = row.endDate
		}

		if row.tierName != "" {
			if _, seen := seenTierNames[row.tierName]; !seen {
				seenTierNames[row.tierName] = struct{}{}
				summary.TierNames = append(summary.TierNames, row.tierName)
			}
		}
		if row.status != "" {
			if _, seen := seenStatuses[row.status]; !seen {
				seenStatuses[row.status] = struct{}{}
				summary.Statuses = append(summary.Statuses, row.status)
			}
		}
	}

	current, ok := currentMembershipRow(sorted)
	if !ok {
		return summary
	}

	summary.B2BOrgUID = current.orgUID
	summary.CompanyName = current.companyName
	summary.ProjectUID = current.projectUID
	summary.ProjectSlug = current.projectSlug
	summary.CurrentStatus = current.status
	summary.CurrentTierName = current.tierName
	summary.CurrentStart = current.startDate
	summary.CurrentEnd = current.endDate
	summary.CurrentMembershipUID = current.uid

	return summary
}

// currentMembershipRow returns the record the summary reports as current: the
// latest-starting active record, or the latest-starting record when none is
// active. The rows must already be sorted oldest first.
func currentMembershipRow(sorted []membershipRow) (membershipRow, bool) {
	if len(sorted) == 0 {
		return membershipRow{}, false
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(sorted[i].status), membershipStatusActive) {
			return sorted[i], true
		}
	}
	return sorted[len(sorted)-1], true
}

// newMembershipRow reads the fold's fields off one indexed record.
func newMembershipRow(raw map[string]any) membershipRow {
	return membershipRow{
		uid:         membershipField(raw, membershipFieldUID),
		orgUID:      membershipField(raw, membershipFieldOrgUID),
		companyName: membershipField(raw, membershipFieldCompanyName),
		projectUID:  membershipField(raw, membershipFieldProjectUID),
		projectSlug: membershipField(raw, membershipFieldProjectSlug),
		status:      membershipField(raw, membershipFieldStatus),
		tierName:    membershipField(raw, membershipFieldTierName),
		tierRange:   membershipField(raw, membershipFieldTierRange),
		startDate:   membershipField(raw, membershipFieldStartDate),
		endDate:     membershipField(raw, membershipFieldEndDate),
		createdAt:   membershipField(raw, membershipFieldCreatedAt),
	}
}

// membershipField returns a record field as a string; anything else, including
// a missing field, reads as empty.
func membershipField(raw map[string]any, field string) string {
	value, ok := raw[field].(string)
	if !ok {
		return ""
	}
	return value
}

// groupKey folds the record under its organization UID, or its company name
// when the UID is missing, and under its project UID, or its project slug when
// the UID is missing. Labels are trimmed and case-folded so the same
// organization or project keeps one summary. Each half names the kind of
// identity it holds, so a UID never collides with a label that happens to
// spell the same.
func (r membershipRow) groupKey() membershipGroupKey {
	return membershipGroupKey{
		org:     membershipIdentityKey(r.orgUID, r.companyName),
		project: membershipIdentityKey(r.projectUID, r.projectSlug),
	}
}

// membershipIdentityKey keys one half of a group: the UID when the record
// carries one, else its normalized label, each in its own namespace.
func membershipIdentityKey(uid, label string) string {
	if uid != "" {
		return "uid:" + uid
	}
	return "label:" + membershipLabelKey(label)
}

// membershipLabelKey normalizes a label used in place of a missing UID.
func membershipLabelKey(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

// before orders records oldest first, by start date, then creation date, then
// UID, all compared as opaque strings.
func (r membershipRow) before(other membershipRow) bool {
	switch {
	case r.startDate != other.startDate:
		return r.startDate < other.startDate
	case r.createdAt != other.createdAt:
		return r.createdAt < other.createdAt
	default:
		return r.uid < other.uid
	}
}

// term returns the record as it appears in the summary.
func (r membershipRow) term() model.MembershipTerm {
	return model.MembershipTerm{
		MembershipUID: r.uid,
		Status:        r.status,
		TierName:      r.tierName,
		TierRange:     r.tierRange,
		StartDate:     r.startDate,
		EndDate:       r.endDate,
	}
}
