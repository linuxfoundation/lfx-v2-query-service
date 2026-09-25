// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	pkgerrors "github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

func TestResourceSearchQueryMembershipSummary(t *testing.T) {

	t.Run("records read on two pages fold into one summary", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["2023-01-02T00:00:00Z","m-1"]`),
				membershipRecord("m-1", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				}),
			),
			membershipPage(nil,
				membershipRecord("m-2", map[string]any{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
			B2BOrgUID:  "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Equal(t, uint64(2), result.TermsTotal)
		require.Len(t, result.Summaries, 1)
		require.Equal(t, uint64(2), result.Summaries[0].TermCount)
		require.Equal(t, "m-2", result.Summaries[0].CurrentMembershipUID)
		require.Equal(t, "2023-01-01T00:00:00Z", result.Summaries[0].FirstStart)
		require.Equal(t, "2025-01-01T00:00:00Z", result.Summaries[0].LastEnd)
		require.Empty(t, result.CacheControl)

		criteria := searcher.QueryResourceCriteria()
		require.Len(t, criteria, 2)
		require.Equal(t, constants.MembershipResourceType, *criteria[0].ResourceType)
		require.Equal(t, []string{"project_uid:proj-1", "b2b_org_uid:org-1"}, criteria[0].TagsAll)
		require.Equal(t, constants.MaxPageSize, criteria[0].PageSize)
		require.Equal(t, membershipSortField, criteria[0].SortBy)
		require.Equal(t, "asc", criteria[0].SortOrder)
		require.Equal(t, "parent_refs", criteria[0].SortBy)
		require.Equal(t, "min", criteria[0].SortMode)
		require.False(t, criteria[0].PublicOnly)
		require.Nil(t, criteria[0].SearchAfter, "the first page starts the keyset")
		require.Equal(t, `["2023-01-02T00:00:00Z","m-1"]`, *criteria[1].SearchAfter,
			"the second page continues from the cursor of the first")
	})

	t.Run("a record read twice is folded once, as the copy read last", func(t *testing.T) {
		membership := func(status string) map[string]any {
			return map[string]any{
				"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
				"project_uid": "proj-1", "project_slug": "example-project",
				"status": status, "tier_name": "Gold",
				"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
				"created_at": "2023-01-02T00:00:00Z",
			}
		}
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["2023-01-02T00:00:00Z","m-1"]`),
				membershipRecord("m-1", membership("Active")),
			),
			// The record was re-indexed between the two pages, so its keyset
			// position moved past the cursor and it is served again. The
			// second copy is what the re-index wrote.
			membershipPage(nil,
				membershipRecord("m-1", membership("Expired")),
			),
		)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Equal(t, uint64(1), result.TermsTotal)
		require.Len(t, result.Summaries, 1)
		require.Equal(t, uint64(1), result.Summaries[0].TermCount)
		require.Len(t, result.Summaries[0].Terms, 1)
		require.Equal(t, []string{"Expired"}, result.Summaries[0].Statuses,
			"the copy read last is the one the re-index wrote")
		require.Equal(t, "Expired", result.Summaries[0].Terms[0].Status)
		require.Equal(t, "Expired", result.Summaries[0].CurrentStatus)
	})

	t.Run("records the caller may not see are left out of the fold", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["2023-01-02T00:00:00Z","m-1"]`),
				membershipRecord("m-1", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
				membershipRecord("m-hidden", map[string]any{
					"uid": "m-hidden", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-2", "project_slug": "other-project",
					"status": "Active", "tier_name": "Platinum",
					"start_date": "2023-02-01T00:00:00Z", "created_at": "2023-02-02T00:00:00Z",
				}),
			),
			membershipPage(nil,
				membershipRecord("m-2", map[string]any{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = ""
		checker.DeniedResourceIDs = []string{constants.MembershipResourceType + ":m-hidden"}
		checker.RecordCheckAccessMessages()
		service := newTestResourceSearch(t, searcher, checker)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Equal(t, uint64(2), result.TermsTotal, "the denied record is not folded")
		require.Len(t, result.Summaries, 1)
		require.Equal(t, "proj-1", result.Summaries[0].ProjectUID)
		require.Equal(t, uint64(2), result.Summaries[0].TermCount)

		require.Equal(t, 2, checker.CheckAccessCalls(), "one batched check per page")
		require.Equal(t, []string{
			"project_membership:m-1#auditor@user:test-user\nproject_membership:m-hidden#auditor@user:test-user",
			"project_membership:m-2#auditor@user:test-user",
		}, checker.CheckAccessMessages())
		require.Equal(t, []string{"b2b_org_uid:org-1"}, searcher.QueryResourceCriteria()[0].TagsAll)
	})

	t.Run("the record cap inside a single run reads it to the end", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["2023-01-02T00:00:00Z","m-2"]`),
				membershipRecord("m-1", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
				membershipRecord("m-2", map[string]any{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
			// The organization continues beyond the cap: the read must
			// fetch its final page before returning a whole summary.
			membershipPage(nil,
				membershipRecord("m-3", map[string]any{
					"uid": "m-3", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Platinum",
					"start_date": "2025-01-01T00:00:00Z", "created_at": "2025-01-02T00:00:00Z",
				}),
			),
		)
		config := DefaultConfig()
		config.MaxSummaryRecords = 2
		service := newTestResourceSearchWithConfig(t, searcher, mock.NewMockAccessControlChecker(), config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Equal(t, uint64(3), result.TermsTotal)
		require.Len(t, result.Summaries, 1)
		require.Equal(t, uint64(3), result.Summaries[0].TermCount)
		require.Equal(t, "m-3", result.Summaries[0].CurrentMembershipUID)
		require.Nil(t, result.SearchAfter)
		require.Equal(t, 2, searcher.QueryResourceCalls(), "the run is read whole even beyond the cap")
	})

	t.Run("a page carrying a cursor counts as a full page against the cap", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			// Two converted records on a page that carries a cursor: the
			// searcher dropped the rest, but the page was read in full.
			// A boundary lets the read stop once that raw page hits the cap.
			membershipPage(cursor(`["b corp","m-2"]`),
				orderedMembershipRecord("m-1", "a corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
				}),
				orderedMembershipRecord("m-2", "b corp", map[string]any{"uid": "m-2", "b2b_org_uid": "org-2"}),
			),
			membershipPage(nil),
		)
		config := DefaultConfig()
		config.MaxSummaryRecords = constants.MaxPageSize
		service := newTestResourceSearchWithConfig(t, searcher, mock.NewMockAccessControlChecker(), config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.False(t, result.Complete)
		require.Equal(t, 1, searcher.QueryResourceCalls(), "a full page reached the cap whatever the searcher converted")
		require.Equal(t, cursor(`["a corp","m-1"]`), result.SearchAfter)
	})

	t.Run("a caller who sees nothing walks past the cap as far as the plain search walks denied pages", func(t *testing.T) {
		hidden := func(uid string) model.Resource {
			return orderedMembershipRecord(uid, "a corp", map[string]any{
				"uid": uid, "b2b_org_uid": "org-1", "company_name": "A Corp",
				"project_uid": "proj-1", "project_slug": "example-project",
				"status": "Active", "tier_name": "Gold",
			})
		}
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["a corp","m-1"]`), hidden("m-1")),
			membershipPage(cursor(`["a corp","m-2"]`), hidden("m-2")),
			membershipPage(cursor(`["a corp","m-3"]`), hidden("m-3")),
			membershipPage(nil, hidden("m-4")),
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DeniedResourceIDs = []string{constants.MembershipResourceType + ":"}
		config := DefaultConfig()
		config.MaxSummaryRecords = 1
		config.DeniedPageWalk = 5
		service := newTestResourceSearchWithConfig(t, searcher, checker, config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete, "an exhausted scope the caller cannot see looks like an absent one")
		require.Nil(t, result.SearchAfter)
		require.Empty(t, result.Summaries)
		require.Equal(t, 4, searcher.QueryResourceCalls(), "the cap did not stop a read that had seen nothing")
	})

	t.Run("a caller who sees nothing gets a boundary continuation only past the denied-page walk", func(t *testing.T) {
		hidden := func(uid string) model.Resource {
			return orderedMembershipRecord(uid, uid, map[string]any{
				"uid": uid, "b2b_org_uid": "org-1", "company_name": "A Corp",
				"project_uid": "proj-1", "project_slug": "example-project",
				"status": "Active", "tier_name": "Gold",
			})
		}
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["a corp","m-1"]`), hidden("m-1")),
			membershipPage(cursor(`["a corp","m-2"]`), hidden("m-2")),
			membershipPage(cursor(`["a corp","m-3"]`), hidden("m-3")),
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DeniedResourceIDs = []string{constants.MembershipResourceType + ":"}
		config := DefaultConfig()
		config.MaxSummaryRecords = 1
		config.DeniedPageWalk = 1
		service := newTestResourceSearchWithConfig(t, searcher, checker, config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
		})

		require.NoError(t, err)
		require.False(t, result.Complete)
		require.NotNil(t, result.SearchAfter, "past the walk the empty read keeps its continuation, as the plain search does")
		require.Empty(t, result.Summaries)
		require.Equal(t, 2, searcher.QueryResourceCalls(), "one page plus the walk")
	})

	t.Run("a record whose later copy carries no data is withdrawn", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		withData := membershipRecord("m-1", map[string]any{
			"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
			"project_uid": "proj-1", "project_slug": "example-project",
			"status": "Active", "tier_name": "Gold",
		})
		withoutData := membershipRecord("m-1", nil)
		withoutData.Data = nil
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["example corp","m-1"]`), withData),
			membershipPage(nil, withoutData),
		)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Equal(t, uint64(0), result.TermsTotal, "the copy read last carried nothing to fold, so the earlier row is withdrawn")
		require.Empty(t, result.Summaries)
	})

	t.Run("the record cap cuts at the last organization boundary and returns the cursor that resumes there", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["b corp","m-3"]`),
				orderedMembershipRecord("m-1", "a corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-a", "company_name": "A Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-2", "a corp", map[string]any{
					"uid": "m-2", "b2b_org_uid": "org-a", "company_name": "A Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-3", "b corp", map[string]any{
					"uid": "m-3", "b2b_org_uid": "org-b", "company_name": "B Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		config := DefaultConfig()
		config.MaxSummaryRecords = 3
		service := newTestResourceSearchWithConfig(t, searcher, mock.NewMockAccessControlChecker(), config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
		})

		require.NoError(t, err)
		require.False(t, result.Complete)
		require.Len(t, result.Summaries, 1, "the organization the read stopped inside is left out")
		require.Equal(t, "org-a", result.Summaries[0].B2BOrgUID)
		require.Equal(t, uint64(2), result.Summaries[0].TermCount)
		require.Equal(t, uint64(2), result.TermsTotal, "terms_total counts the folded records, not the records read")
		require.NotNil(t, result.SearchAfter)
		require.Equal(t, `["a corp","m-2"]`, *result.SearchAfter, "the cursor is the last record before the organization left out")
		require.Equal(t, 1, searcher.QueryResourceCalls())
	})

	t.Run("a record reassigned to the organization the read stopped inside is left out with it", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["b corp","m-2"]`),
				orderedMembershipRecord("m-1", "a corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-a", "company_name": "A Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-2", "b corp", map[string]any{
					"uid": "m-2", "b2b_org_uid": "org-b", "company_name": "B Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
			// The first record was reassigned and re-indexed while the read was
			// between pages, so it is served again, now inside the last
			// organization run of the read.
			membershipPage(cursor(`["c corp","m-3"]`),
				orderedMembershipRecord("m-1", "c corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-c", "company_name": "C Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-3", "c corp", map[string]any{
					"uid": "m-3", "b2b_org_uid": "org-c", "company_name": "C Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		config := DefaultConfig()
		// Two full pages reach the cap: a page that carries a cursor counts
		// as the page size whatever it converted.
		config.MaxSummaryRecords = 2 * constants.MaxPageSize
		service := newTestResourceSearchWithConfig(t, searcher, mock.NewMockAccessControlChecker(), config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
		})

		require.NoError(t, err)
		require.False(t, result.Complete)
		require.NotNil(t, result.SearchAfter)
		require.Equal(t, `["b corp","m-2"]`, *result.SearchAfter)
		require.Len(t, result.Summaries, 1, "the reassigned record belongs to the organization left out")
		require.Equal(t, "org-b", result.Summaries[0].B2BOrgUID)
		require.Equal(t, uint64(1), result.TermsTotal,
			"the resumed read serves the reassigned record again, so this read must not fold it")
	})

	t.Run("the boundary is found over records the caller cannot see", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["b corp","m-3"]`),
				orderedMembershipRecord("m-1", "a corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-a", "company_name": "A Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-hidden", "a corp", map[string]any{
					"uid": "m-hidden", "b2b_org_uid": "org-a", "company_name": "A Corp",
					"project_uid": "proj-2", "project_slug": "other-project",
					"status": "Active", "tier_name": "Platinum",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
				orderedMembershipRecord("m-3", "b corp", map[string]any{
					"uid": "m-3", "b2b_org_uid": "org-b", "company_name": "B Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = ""
		checker.DeniedResourceIDs = []string{constants.MembershipResourceType + ":m-hidden"}
		config := DefaultConfig()
		config.MaxSummaryRecords = 3
		service := newTestResourceSearchWithConfig(t, searcher, checker, config)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "proj-1",
		})

		require.NoError(t, err)
		require.False(t, result.Complete)
		require.Len(t, result.Summaries, 1)
		require.Equal(t, uint64(1), result.TermsTotal, "the hidden record is not folded")
		require.NotNil(t, result.SearchAfter)
		require.Equal(t, `["a corp","m-hidden"]`, *result.SearchAfter, "the cursor may be a record the caller cannot see")
	})

	t.Run("a resumed read starts from the cursor it was given", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(nil,
				orderedMembershipRecord("m-3", "b corp", map[string]any{
					"uid": "m-3", "b2b_org_uid": "org-b", "company_name": "B Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
				}),
			),
		)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID:  "proj-1",
			SearchAfter: cursor(`["a corp","m-2"]`),
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Nil(t, result.SearchAfter)
		require.Len(t, result.Summaries, 1)
		require.Equal(t, "org-b", result.Summaries[0].B2BOrgUID)
		require.Equal(t, cursor(`["a corp","m-2"]`), searcher.QueryResourceCriteria()[0].SearchAfter,
			"the first page continues from the cursor the caller passed back")
	})

	t.Run("an anonymous caller reads no membership record", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(nil,
				membershipRecord("m-1", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
			),
		)
		checker := mock.NewMockAccessControlChecker()
		service := newTestResourceSearch(t, searcher, checker)

		result, err := service.QueryMembershipSummary(membershipContext(constants.AnonymousPrincipal), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.NoError(t, err)
		require.True(t, result.Complete)
		require.Zero(t, result.TermsTotal)
		require.Empty(t, result.Summaries)
		require.Equal(t, constants.AnonymousCacheControlHeader, result.CacheControl)
		require.Zero(t, checker.CheckAccessCalls(), "public records need no access check")
		require.True(t, searcher.QueryResourceCriteria()[0].PublicOnly)
	})

	t.Run("a failed access check fails the whole read", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(nil,
				membershipRecord("m-1", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
			),
		)
		checker := mock.NewMockAccessControlChecker()
		checker.SetCheckAccessError(stderrors.New("checker unavailable"))
		service := newTestResourceSearch(t, searcher, checker)

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		var unavailable pkgerrors.ServiceUnavailable
		require.ErrorAs(t, err, &unavailable)
		require.Nil(t, result, "a partial summary is never returned")
	})

	t.Run("a cursor that does not advance is an adapter defect, not a token that never moves", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		stuck := membershipPage(cursor(`["example corp","m-1"]`),
			orderedMembershipRecord("m-1", "example corp", map[string]any{
				"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
				"project_uid": "proj-1", "project_slug": "example-project",
				"status": "Active", "tier_name": "Gold",
				"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
			}),
		)
		searcher.SetQueryResourcePages(stuck, stuck, stuck)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 2, searcher.QueryResourceCalls(), "stops as soon as the cursor repeats instead of reading to the cap")
	})

	t.Run("a cancelled read stops between pages", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		searcher.SetQueryResourcePages(
			membershipPage(cursor(`["example corp","m-1"]`),
				orderedMembershipRecord("m-1", "example corp", map[string]any{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2023-01-01T00:00:00Z", "created_at": "2023-01-02T00:00:00Z",
				}),
			),
			membershipPage(nil),
		)
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())
		cancelled, cancel := context.WithCancel(membershipContext("test-user"))
		cancel()

		result, err := service.QueryMembershipSummary(cancelled, model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, 1, searcher.QueryResourceCalls(), "no further page is fetched once the context is done")
	})

	t.Run("a read without a scope is a validation error", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{})

		var validation pkgerrors.Validation
		require.ErrorAs(t, err, &validation)
		require.Nil(t, result)
		require.Zero(t, searcher.QueryResourceCalls(), "an unscoped read never reaches the index")
	})

	t.Run("a caller without a principal is a validation error", func(t *testing.T) {
		service := newTestResourceSearch(t, mock.NewMockResourceSearcher(), mock.NewMockAccessControlChecker())

		result, err := service.QueryMembershipSummary(context.Background(), model.MembershipSummaryCriteria{
			B2BOrgUID: "org-1",
		})

		var validation pkgerrors.Validation
		require.ErrorAs(t, err, &validation)
		require.Nil(t, result)
	})
}

func TestConfigValidateSummaryRecordCap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		records   int
		errorText string
	}{
		{"the default cap is valid", constants.DefaultMaxSummaryRecords, ""},
		{"one record is the smallest read", 1, ""},
		{"the configurable maximum is valid", constants.MaxSummaryRecordCap, ""},
		{"a negative cap is refused", -1, "max summary records must be positive, got -1"},
		{"a cap beyond the maximum is refused", constants.MaxSummaryRecordCap + 1, "max summary records must not exceed 50000, got 50001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaxSummaryRecords = tc.records
			err := config.Validate()
			if tc.errorText == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.errorText)
			}
		})
	}
}

// membershipContext returns a request context carrying the principal the
// security handler would have stored.
func membershipContext(principal string) context.Context {
	return context.WithValue(context.Background(), constants.PrincipalContextID, principal)
}

// cursor returns a keyset cursor as the searcher hands it back.
func cursor(sortValues string) *string {
	return &sortValues
}

// membershipPage builds one page of membership records; a page carrying a
// cursor has a next page.
func membershipPage(searchAfter *string, resources ...model.Resource) *model.SearchResult {
	return &model.SearchResult{
		Resources:       resources,
		NextSearchAfter: searchAfter,
		Total:           len(resources),
	}
}

// membershipRecord builds one indexed membership record with the access-check
// pair the batched check is built from: the record's own object with the
// auditor relation, as the member service indexes it.
func membershipRecord(uid string, data map[string]any) model.Resource {
	return model.Resource{
		Type: constants.MembershipResourceType,
		ID:   uid,
		Data: data,
		TransactionBodyStub: model.TransactionBodyStub{
			ObjectRef:           constants.MembershipResourceType + ":" + uid,
			ObjectType:          constants.MembershipResourceType,
			ObjectID:            uid,
			Public:              false,
			AccessCheckObject:   constants.MembershipResourceType + ":" + uid,
			AccessCheckRelation: "auditor",
		},
	}
}

// orderedMembershipRecord builds one indexed membership record as the
// searcher returns it for the summary read: with the sort values of the
// organization order, an opaque run key first and the record id second.
func orderedMembershipRecord(uid, runKey string, data map[string]any) model.Resource {
	record := membershipRecord(uid, data)
	record.SortValues = `["` + runKey + `","` + uid + `"]`
	return record
}

// TestResourceSearchMembershipSummaryCapResume composes a capped read with
// the read its own cursor produces: the two halves together are exactly the
// uncapped read of the same fixture, nothing lost and nothing folded twice.
// The searcher serves pages keyed by the incoming cursor, as the adapter
// does, so a boundary cursor one hit off would show as a gap or a repeat.
func TestResourceSearchMembershipSummaryCapResume(t *testing.T) {
	record := func(uid, sortName, orgUID, company, tier string) model.Resource {
		return orderedMembershipRecord(uid, sortName, map[string]any{
			"uid": uid, "b2b_org_uid": orgUID, "company_name": company,
			"project_uid": "proj-1", "project_slug": "example-project",
			"status": "Active", "tier_name": tier,
			"start_date": "2024-01-01T00:00:00Z", "created_at": "2024-01-02T00:00:00Z",
		})
	}
	// Three organizations over three pages; the second page opens a new
	// organization, so a read capped inside it has a boundary to cut at.
	// A page that carries a cursor counts as a full page against the cap,
	// so the caps are set in whole pages.
	pages := map[string]*model.SearchResult{
		"": page(`["a corp","m-2"]`,
			record("m-1", "a corp", "org-a", "A Corp", "Gold"),
			record("m-2", "a corp", "org-a", "A Corp", "Silver"),
		),
		`["a corp","m-2"]`: page(`["b corp","m-4"]`,
			record("m-3", "b corp", "org-b", "B Corp", "Gold"),
			record("m-4", "b corp", "org-b", "B Corp", "Platinum"),
		),
		`["b corp","m-4"]`: page("",
			record("m-5", "c corp", "org-c", "C Corp", "Gold"),
		),
	}
	newService := func(t *testing.T, cap int) (*ResourceSearch, *pagedSearcher) {
		t.Helper()
		searcher := &pagedSearcher{MockResourceSearcher: mock.NewMockResourceSearcher(), pages: pages, errAt: map[string]error{}}
		config := DefaultConfig()
		config.MaxSummaryRecords = cap
		svc, err := NewResourceSearch(searcher, mock.NewMockAccessControlChecker(), mock.NewMockResourceFilter(), config)
		require.NoError(t, err)
		return svc.(*ResourceSearch), searcher
	}
	terms := func(result *model.MembershipSummaryResult) map[string]uint64 {
		out := map[string]uint64{}
		for _, summary := range result.Summaries {
			require.NotContains(t, out, summary.B2BOrgUID, "an organization is folded once per read")
			out[summary.B2BOrgUID] = summary.TermCount
		}
		return out
	}

	whole, _ := newService(t, 3*constants.MaxPageSize+1)
	uncapped, err := whole.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{ProjectUID: "proj-1"})
	require.NoError(t, err)
	require.True(t, uncapped.Complete)
	require.Equal(t, map[string]uint64{"org-a": 2, "org-b": 2, "org-c": 1}, terms(uncapped))

	capped, searcher := newService(t, 2*constants.MaxPageSize)
	first, err := capped.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{ProjectUID: "proj-1"})
	require.NoError(t, err)
	require.False(t, first.Complete)
	require.NotNil(t, first.SearchAfter)
	require.Equal(t, map[string]uint64{"org-a": 2}, terms(first), "the organization the cap fell inside is left for the resumed read")

	second, err := capped.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
		ProjectUID:  "proj-1",
		SearchAfter: first.SearchAfter,
	})
	require.NoError(t, err)
	require.True(t, second.Complete)
	require.Nil(t, second.SearchAfter)
	require.Equal(t, []string{"", *first.SearchAfter, *first.SearchAfter, `["b corp","m-4"]`}, searcher.cursors,
		"the capped read stopped inside the page its cursor names, and the resumed read asks for that page again")

	union := terms(first)
	for org, count := range terms(second) {
		require.NotContains(t, union, org, "an organization is folded in one half only")
		union[org] = count
	}
	require.Equal(t, terms(uncapped), union, "the capped read and its resume are exactly the uncapped read")
	require.Equal(t, uncapped.TermsTotal, first.TermsTotal+second.TermsTotal)
}

// TestResourceSearchMembershipSummaryWholeRunWalk checks real-size pages and
// cursor-based resumes, including a multi-page first run and label fallbacks.
func TestResourceSearchMembershipSummaryWholeRunWalk(t *testing.T) {
	var records []model.Resource
	for _, run := range []struct {
		name, org string
		count     int
	}{
		{"a corp", "org-a", 2500},
		{"b corp", "", 1200},
		{"c corp", "org-c", 2500},
		{"d corp", "org-d", 30},
	} {
		for i := 0; i < run.count; i++ {
			uid := fmt.Sprintf("m-%05d", len(records))
			project := fmt.Sprintf("proj-%d", i%2)
			projectUID := project
			if run.org == "" {
				projectUID = "" // Exercise both halves of the label fallback.
			}
			records = append(records, orderedMembershipRecord(uid, run.name, map[string]any{
				"uid": uid, "company_name": run.name, "b2b_org_uid": run.org,
				"project_uid": projectUID, "project_slug": project,
				"status": "Active", "tier_name": fmt.Sprintf("Tier %d", i%3),
				"start_date": "2024-01-01", "created_at": fmt.Sprintf("2024-01-%02d", 1+i%28),
			}))
		}
	}
	newService := func(cap int) (*ResourceSearch, *pagedSearcher) {
		searcher := membershipFixtureSearcher(records)
		config := DefaultConfig()
		config.MaxSummaryRecords = cap
		svc, err := NewResourceSearch(searcher, mock.NewMockAccessControlChecker(), mock.NewMockResourceFilter(), config)
		require.NoError(t, err)
		return svc.(*ResourceSearch), searcher
	}
	whole, _ := newService(constants.MaxSummaryRecordCap)
	uncapped, err := whole.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{ProjectUID: "scope"})
	require.NoError(t, err)
	require.True(t, uncapped.Complete)

	capped, searcher := newService(constants.MaxPageSize)
	var after *string
	var union []model.MembershipTermSummary
	var total uint64
	seen := make(map[[2]string]bool)
	reads := 0
	for {
		reads++
		require.LessOrEqual(t, reads, 4, "a walk must advance and terminate")
		result, readErr := capped.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{
			ProjectUID: "scope", SearchAfter: after,
		})
		require.NoError(t, readErr)
		if reads == 1 {
			require.False(t, result.Complete)
			require.Equal(t, uint64(2500), result.TermsTotal, "the first run spans three pages and is returned whole")
			require.Len(t, searcher.cursors, 3)
			require.Equal(t, cursor(records[2499].SortValues), result.SearchAfter, "resume before the second run, not after the last page")
		}
		for _, summary := range result.Summaries {
			org, project := "uid:"+summary.B2BOrgUID, "uid:"+summary.ProjectUID
			if summary.B2BOrgUID == "" {
				org = "label:" + summary.CompanyName
			}
			if summary.ProjectUID == "" {
				project = "label:" + summary.ProjectSlug
			}
			key := [2]string{org, project}
			require.False(t, seen[key], "a summary key must never appear in more than one read: %v", key)
			seen[key] = true
			union = append(union, summary)
		}
		total += result.TermsTotal
		if result.Complete {
			require.Nil(t, result.SearchAfter)
			break
		}
		require.NotNil(t, result.SearchAfter)
		require.NotEqual(t, after, result.SearchAfter)
		after = result.SearchAfter
	}
	require.Equal(t, 3, reads)
	require.Equal(t, uncapped.TermsTotal, total)
	require.ElementsMatch(t, uncapped.Summaries, union, "the entire fold, not just term counts, equals a single uncapped read")
}

// membershipFixtureSearcher serves full pages keyed by cursor, including pages
// starting at each run boundary a capped read could hand back to the caller.
func membershipFixtureSearcher(records []model.Resource) *pagedSearcher {
	pages := make(map[string]*model.SearchResult)
	for start := range records {
		if start > 0 && membershipRunKey(records[start-1].SortValues) == membershipRunKey(records[start].SortValues) {
			continue
		}
		for offset := start; offset < len(records); offset += constants.MaxPageSize {
			key := ""
			if offset > 0 {
				key = records[offset-1].SortValues
			}
			end := min(offset+constants.MaxPageSize, len(records))
			next := ""
			if end-offset == constants.MaxPageSize {
				next = records[end-1].SortValues
			}
			pages[key] = page(next, records[offset:end]...)
		}
	}
	return &pagedSearcher{MockResourceSearcher: mock.NewMockResourceSearcher(), pages: pages}
}

func TestResourceSearchMembershipSummaryRunCeiling(t *testing.T) {
	for _, mode := range []string{"visible", "denied", "unconverted"} {
		t.Run(mode, func(t *testing.T) {
			records := make([]model.Resource, constants.MaxSummaryRunRecords+1)
			for i := range records {
				uid := fmt.Sprintf("m-%05d", i)
				records[i] = orderedMembershipRecord(uid, "one corp", map[string]any{
					"uid": uid, "company_name": "One Corp", "b2b_org_uid": "org-1", "project_uid": "proj-1",
				})
			}
			searcher := membershipFixtureSearcher(records)
			if mode == "unconverted" {
				for _, page := range searcher.pages {
					page.Resources = nil
				}
			}
			checker := mock.NewMockAccessControlChecker()
			if mode == "denied" {
				checker.DeniedResourceIDs = []string{constants.MembershipResourceType + ":"}
			}
			config := DefaultConfig()
			config.MaxSummaryRecords = 1
			service, err := NewResourceSearch(searcher, checker, mock.NewMockResourceFilter(), config)
			require.NoError(t, err)
			result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{B2BOrgUID: "org-1"})
			var unavailable pkgerrors.ServiceUnavailable
			require.ErrorAs(t, err, &unavailable)
			require.ErrorContains(t, err, "record ceiling")
			require.Nil(t, result, "no partial summaries escape the hard ceiling")
			require.Len(t, searcher.cursors, constants.MaxSummaryRunRecords/constants.MaxPageSize)
		})
	}
}

// TestResourceSearchMembershipSummarySearcherFailure pins that a searcher
// failure ends the read as an error rather than as a shorter summary.
func TestResourceSearchMembershipSummarySearcherFailure(t *testing.T) {
	searcher := mock.NewMockResourceSearcher()
	searcher.SetQueryResourcesError(stderrors.New("opensearch unavailable"))
	service := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())

	result, err := service.QueryMembershipSummary(membershipContext("test-user"), model.MembershipSummaryCriteria{ProjectUID: "proj-1"})

	require.Error(t, err)
	require.ErrorContains(t, err, "opensearch unavailable")
	require.Nil(t, result)
}

// TestMembershipRunTrackerKeepsAnEarlierBoundary pins that a run change
// after a hit the searcher did not order does not discard a boundary the
// tracker already found.
func TestMembershipRunTrackerKeepsAnEarlierBoundary(t *testing.T) {
	tracker := membershipRunTracker{}
	tracker.observe(`["a corp","m-1"]`)
	tracker.observe(`["b corp","m-2"]`)
	boundary, ok := tracker.boundary()
	require.True(t, ok)
	require.Equal(t, `["a corp","m-1"]`, boundary)

	// The unordered hit opens a run of its own after the last ordered hit,
	// which is a boundary; the run change out of it carries no cursor and
	// must leave that boundary standing.
	tracker.observe("")
	tracker.observe(`["c corp","m-4"]`)
	boundary, ok = tracker.boundary()
	require.True(t, ok, "an unordered hit does not throw the boundary away")
	require.Equal(t, `["b corp","m-2"]`, boundary)
}
