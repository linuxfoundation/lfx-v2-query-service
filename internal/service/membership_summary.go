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
// have been read. The records are read in organization order, so a read that
// stops at the cap folds every organization it has read whole, leaves out the
// organization it stopped inside, and returns the cursor that resumes there;
// a later read passing that cursor continues with the next organizations.
// When the whole read fell inside a single run of records sharing one company
// name, usually one organization, there is no boundary to cut at: the read
// keeps the run as far as it was read and returns the cursor of the last
// hit, so the run may continue in the next read. A failed access check fails the whole read: a summary is
// never returned as if whole while part of the caller's visibility is unknown.
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
				// that copy too: a re-index that renamed the company moved
				// the record into another run, and the row moves with it.
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
		if recordsRead >= s.config.MaxSummaryRecords && (anyVisible || pages > s.config.DeniedPageWalk) {
			// The cap is checked after a whole page, so pages are never
			// split and the cap may be overshot by up to one page. While the
			// caller has seen nothing, the cap yields to the denied-page
			// walk: the read keeps going as far as the plain search walks
			// denied pages before it exposes a continuation, so a scope the
			// caller cannot see and a scope that does not exist stay
			// indistinguishable to the same extent. The worst case for such
			// a read is therefore the larger of the cap and one page plus
			// the walk.
			boundary, canResume := runs.boundary()
			if canResume {
				// Leave out the organization the read stopped inside: its
				// records may continue on the next page, and the resumed
				// read starts with it. A record re-served under a new
				// company name sits at the row of its first copy, which may
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
			} else {
				// The whole read fell inside one run, so there is no
				// organization boundary to cut at. Rather than strand the
				// organizations that sort after it, keep the run as far as
				// it was read and continue from the last hit: the run may
				// go on in the next read.
				resume = page.NextSearchAfter
			}
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
// and the cheapest scope for the read. The order is the record's sortable
// name, which the member service indexes as the company name lowercased (see
// its indexer contract), so the records of one organization are read together
// and a read that stops at the cap can resume at the next organization; the
// record id breaks ties.
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
		SearchAfter:  criteria.SearchAfter,
	}
}

// membershipSortField is the indexed field the summary read orders on: the
// record's sortable name, which carries the company name lowercased, as the
// member service indexes it.
const membershipSortField = "sort_name"

// membershipRunKey returns the organization a hit belongs to for the purpose
// of the read order: the first of its sort values, which is the sortable
// name the read orders on. A hit without sort values keys as empty, so hits
// the searcher did not order all fall in one run.
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
// last hit before that organization began.
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
		r.boundaryCursor = r.previous
		r.hasBoundary = r.previous != ""
	}
	r.previous = sortValues
}

// boundary returns the cursor a resumed read continues from, at the start of
// the current run, and whether there is one: a read that has seen a single
// run, or hits the searcher did not order, has nothing to resume from.
func (r *membershipRunTracker) boundary() (string, bool) {
	if !r.hasBoundary {
		return "", false
	}
	return r.boundaryCursor, true
}
