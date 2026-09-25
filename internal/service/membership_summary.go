// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

// QueryMembershipSummary folds the membership records of an organization, a
// project, or both into one summary per organization and project.
//
// The records are read page by page with the same visibility as a plain
// search: each page is access-checked in one batched request, and only the
// records the caller may see reach the fold. An anonymous caller reads public
// records alone. The read continues from the keyset cursor of the previous
// page until a page carries none, or until config.MaxSummaryRecords records
// have been read and an organization boundary is available. A capped read
// folds only whole runs, leaves out the trailing run, and returns the cursor
// that resumes at its start. If the cap falls inside the first run, the read
// continues until a boundary appears or the pages run out. If neither happens
// within constants.MaxSummaryRunRecords raw hits, the read fails rather than
// returning a partial run. A failed access check also fails the whole read:
// a summary is never returned as if whole while part of it is unknown.
func (s *ResourceSearch) QueryMembershipSummary(ctx context.Context, criteria model.MembershipSummaryCriteria) (*model.MembershipSummaryResult, error) {

	started := time.Now()

	// As on the plain search, Goa cannot express "at least one of these
	// fields must be set", so the scope is checked here as well as at the
	// transport boundary: an unscoped read would drain every membership
	// record in the index.
	if criteria.ProjectUID == "" && criteria.B2BOrgUID == "" {
		slog.ErrorContext(ctx, "membership summary criteria validation failed")
		return nil, errors.NewValidation("at least one summary parameter must be provided: project_uid or b2b_org_uid")
	}

	// Grab the principal which was stored into the context by the security handler.
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		// This should not happen; the Auther always sets this or errors.
		return nil, errors.NewValidation("missing principal in context")
	}

	searchCriteria := membershipSearchCriteria(criteria)
	anonymous := principal == constants.AnonymousPrincipal
	if anonymous {
		// For an anonymous user, the "public:true" OpenSearch term filter
		// stands in for the access check, as it does on the plain search.
		searchCriteria.PublicOnly = true
	}

	slog.DebugContext(ctx, "starting membership summary read",
		"scope_tags", searchCriteria.TagsAll,
		"resumed", criteria.SearchAfter != nil,
	)

	var (
		rows        []map[string]any
		rowRuns     []string
		folded      = make(map[string]int)
		recordsRead int
		pages       int
		complete    bool
		resume      *string
		runs        membershipRunTracker
		anyVisible  bool
	)

	for {
		pages++
		page, err := s.resourceSearcher.QueryResources(ctx, searchCriteria)
		if err != nil {
			slog.ErrorContext(ctx, "search operation failed while reading membership records",
				"error", err,
				"page", pages,
			)
			return nil, fmt.Errorf("search operation failed: %w", err)
		}

		message := s.BuildMessage(ctx, principal, page)
		visible, errCheckAccess := s.CheckAccess(ctx, principal, page.Resources, message)
		if errCheckAccess != nil {
			slog.ErrorContext(ctx, "membership summary access control check failed",
				"error", errCheckAccess,
				"page", pages,
			)
			return nil, errors.NewServiceUnavailable("access control check failed", errCheckAccess)
		}

		if page.NextSearchAfter != nil {
			// A page that carries a cursor was full: the searcher hands a
			// cursor back only when the page held as many hits as asked.
			// Counting the page size rather than the converted records keeps
			// the cap a bound on the hits read, even when the searcher
			// dropped a document it could not convert.
			recordsRead += searchCriteria.PageSize
		} else {
			recordsRead += len(page.Resources)
		}
		// The organization runs are tracked over every hit, visible or not:
		// where one organization ends and the next begins is a property of
		// the index order, not of the caller's visibility.
		for _, hit := range page.Resources {
			runs.observe(hit.SortValues)
		}
		boundary, canResume := runs.boundary()
		// Without a boundary, no part of this read can be safely returned.
		// Count raw hits, not just visible/converted records, so denied or
		// unconvertible pages cannot make the whole-run extension unbounded.
		if !canResume && (recordsRead > constants.MaxSummaryRunRecords ||
			(recordsRead >= constants.MaxSummaryRunRecords && page.NextSearchAfter != nil)) {
			return nil, errors.NewServiceUnavailable("membership summary run did not end within the record ceiling")
		}
		anyVisible = anyVisible || len(visible) > 0
		for _, resource := range visible {
			identity := resource.ObjectRef
			if identity == "" {
				identity = resource.Type + ":" + resource.ID
			}
			data, isMap := resource.Data.(map[string]any)
			if !isMap {
				// A record without a data object carries nothing to fold. If
				// an earlier copy of it was folded, the copy read last still
				// wins: the earlier row is withdrawn.
				if row, alreadyFolded := folded[identity]; alreadyFolded {
					rows[row] = nil
				}
				continue
			}
			if row, alreadyFolded := folded[identity]; alreadyFolded {
				// A record re-indexed while the read is between pages can be
				// served a second time. One record is one term, whichever
				// page it arrived on, and the copy read last is the one the
				// re-index wrote. The organization it belongs to comes from
				// that copy too: a re-index that changed the organization
				// reference moved the record into another run, and the row
				// moves with it. A company-name change alone does not.
				rows[row] = data
				rowRuns[row] = membershipRunKey(resource.SortValues)
				continue
			}
			folded[identity] = len(rows)
			rows = append(rows, data)
			rowRuns = append(rowRuns, membershipRunKey(resource.SortValues))
		}

		if page.NextSearchAfter == nil {
			// The last page of the read: there is nothing to continue from.
			complete = true
			break
		}
		if searchCriteria.SearchAfter != nil && *searchCriteria.SearchAfter == *page.NextSearchAfter {
			// The cursor must move, as on the plain search: a cursor that
			// comes back unchanged is an adapter defect, and following it
			// would re-read the same page up to the cap and then hand the
			// caller a page token that never advances.
			slog.ErrorContext(ctx, "search_after cursor did not advance while reading membership records",
				"page", pages,
			)
			return nil, fmt.Errorf("search_after cursor did not advance while reading membership records")
		}
		if errCtx := ctx.Err(); errCtx != nil {
			return nil, fmt.Errorf("membership summary read cancelled: %w", errCtx)
		}
		if recordsRead >= s.config.MaxSummaryRecords && canResume && (anyVisible || pages > s.config.DeniedPageWalk) {
			// The cap is checked after a whole page, so pages are never
			// split and the cap may be overshot by up to one page. While the
			// caller has seen nothing, the cap yields to the denied-page
			// walk: the read keeps going as far as the plain search walks
			// denied pages before it exposes a continuation, so a scope the
			// caller cannot see and a scope that does not exist stay
			// indistinguishable to the same extent. The worst case for such
			// a read is the larger of the cap rounded up to whole pages
			// and the walk plus one page, extended when necessary to find
			// the first run boundary within MaxSummaryRunRecords.
			// Leave out the organization the read stopped inside: its
			// records may continue on the next page, and the resumed
			// read starts with it. A record re-served under a new
			// organization reference sits at the row of its first copy, which may
			// be anywhere, so the run is dropped wherever it lies rather
			// than only off the tail.
			kept := rows[:0]
			for row := range rows {
				if rows[row] != nil && rowRuns[row] != runs.current {
					kept = append(kept, rows[row])
				}
			}
			rows = kept
			resume = &boundary
			slog.WarnContext(ctx, "membership summary stopped at the record cap",
				"pages", pages,
				"records_read", recordsRead,
				"record_cap", s.config.MaxSummaryRecords,
				"at_run_boundary", canResume,
			)
			break
		}
		searchCriteria.SearchAfter = page.NextSearchAfter
	}

	rows = withdrawnRowsRemoved(rows)
	result := &model.MembershipSummaryResult{
		Summaries:   foldMembershipTerms(rows),
		TermsTotal:  uint64(len(rows)),
		Complete:    complete,
		SearchAfter: resume,
	}
	if anonymous {
		// Set a cache control header for anonymous users.
		result.CacheControl = constants.AnonymousCacheControlHeader
	}

	slog.InfoContext(ctx, "membership summary completed",
		"scope_tags", searchCriteria.TagsAll,
		"pages", pages,
		"records_read", recordsRead,
		"terms_total", result.TermsTotal,
		"summaries", len(result.Summaries),
		"complete", result.Complete,
		"resumable", result.SearchAfter != nil,
		"elapsed", time.Since(started),
	)

	return result, nil
}

// withdrawnRowsRemoved drops the rows a later copy of the record withdrew.
func withdrawnRowsRemoved(rows []map[string]any) []map[string]any {
	kept := rows[:0]
	for _, row := range rows {
		if row != nil {
			kept = append(kept, row)
		}
	}
	return kept
}

// membershipSearchCriteria builds the search the summary drains: the
// membership records carrying the requested scope tags, in whole pages, in
// organization order. The scope is expressed as the index tags rather than
// data filters: the same keyword terms the membership catalog recipes use,
// and the cheapest scope for the read. The member-service indexer contract
// gives memberships only b2b_org:<uid> and project:<uid> parent refs. Sorting
// on the minimum ref puts an organization's records together regardless of
// company name, because b2b_org: sorts before project:. Without an organization,
// the project ref groups its organization-less records; ref-less records sort
// last as one run. The record id breaks ties.
func membershipSearchCriteria(criteria model.MembershipSummaryCriteria) model.SearchCriteria {
	resourceType := constants.MembershipResourceType

	tagsAll := make([]string, 0, 2)
	if criteria.ProjectUID != "" {
		tagsAll = append(tagsAll, constants.MembershipProjectTagPrefix+criteria.ProjectUID)
	}
	if criteria.B2BOrgUID != "" {
		tagsAll = append(tagsAll, constants.MembershipOrgTagPrefix+criteria.B2BOrgUID)
	}

	return model.SearchCriteria{
		ResourceType: &resourceType,
		TagsAll:      tagsAll,
		PageSize:     constants.MaxPageSize,
		SortBy:       membershipSortField,
		SortOrder:    "asc",
		SortMode:     "min",
		SearchAfter:  criteria.SearchAfter,
	}
}

// membershipSortField is the multi-valued keyword field the summary orders
// on, selecting its minimum parent ref rather than the mutable company name.
const membershipSortField = "parent_refs"

// membershipRunKey returns the first sort value: the minimum parent ref, or
// null for ref-less records. It is independent of company name. A hit without
// sort values keys as empty, so hits the searcher did not order share a run.
func membershipRunKey(sortValues string) string {
	if sortValues == "" {
		return ""
	}
	var values []any
	if err := json.Unmarshal([]byte(sortValues), &values); err != nil || len(values) == 0 {
		return ""
	}
	if key, isString := values[0].(string); isString {
		return key
	}
	return fmt.Sprint(values[0])
}

// membershipRunTracker follows the organization runs of a read in index
// order: which organization the last hit belongs to, and the cursor of the
// last hit before that organization began. Until a resumable boundary is
// found, the read must continue to the run's end or fail at the hard ceiling;
// it must never return a cursor inside that run.
type membershipRunTracker struct {
	// current is the run key of the last hit observed.
	current string
	// started is true once a hit has been observed.
	started bool
	// previous is the cursor of the hit observed before the current one.
	previous string
	// boundaryCursor is the cursor of the last hit of the run before the
	// current one; a resumed read passing it starts with the current run.
	boundaryCursor string
	// hasBoundary is true once a second run has begun.
	hasBoundary bool
}

// observe records one hit, in read order.
func (r *membershipRunTracker) observe(sortValues string) {
	key := membershipRunKey(sortValues)
	switch {
	case !r.started:
		r.started = true
		r.current = key
	case key != r.current:
		r.current = key
		// A run change after a hit the searcher did not order carries no
		// cursor to resume from; a boundary found earlier in the read
		// stands rather than being thrown away.
		if r.previous != "" {
			r.boundaryCursor = r.previous
			r.hasBoundary = true
		}
	}
	r.previous = sortValues
}

// boundary returns the cursor a resumed read continues from, at the start of
// the current run, and whether there is one: a read that has seen a single
// run, or hits the searcher did not order, must keep reading rather than
// returning a partial run.
func (r *membershipRunTracker) boundary() (string, bool) {
	if !r.hasBoundary {
		return "", false
	}
	return r.boundaryCursor, true
}
