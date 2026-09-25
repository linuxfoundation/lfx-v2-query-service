// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestMembershipParentRefOrder(t *testing.T) {
	// Source: lfx-v2-member-service/docs/indexer-contract.md, Project
	// Membership / Parent References. Only b2b_org:<uid> and project:<uid>
	// are emitted (each when set), so min(parent_refs) is the organization
	// ref whenever one exists, independent of UID spelling or array order.
	for _, org := range []string{"b2b_org:000", "b2b_org:ZZZ", "b2b_org:zzz"} {
		for _, project := range []string{"project:000", "project:AAA", "project:zzz"} {
			require.Less(t, org, project)
			require.Equal(t, org, slices.Min([]string{project, org}))
			require.Equal(t, org, slices.Min([]string{org, project}))
		}
	}
}

func TestResourceSearchMembershipSummaryOrganizationOrder(t *testing.T) {
	var records []membershipOrderedFixture
	add := func(org, project, name string, count int) {
		for range count {
			uid := fmt.Sprintf("m-%05d", len(records))
			var refs []string
			// Deliberately store the project first; min is not array[0].
			if project != "" {
				refs = append(refs, "project:"+project)
			}
			if org != "" {
				refs = append(refs, "b2b_org:"+org)
			}
			records = append(records, membershipOrderedFixture{
				resource: membershipRecord(uid, map[string]any{
					"uid": uid, "b2b_org_uid": org, "project_uid": project,
					"company_name": name, "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01", "created_at": "2024-01-02T00:00:00Z",
				}),
				parentRefs: refs,
			})
		}
	}
	// The cap falls between the old and new spelling. With name order,
	// another organization sorts between them and splits the same pair.
	add("org-a", "proj-1", "A Old Company", constants.MaxPageSize)
	add("org-a", "proj-1", "Z New Company", constants.MaxPageSize+500)
	add("org-b", "proj-1", "M Middle Company", 50)
	// Label-keyed pairs must also stay whole across multiple pages.
	for range 1250 {
		add("", "proj-1", "Unknown A", 1)
		add("", "proj-1", "Unknown B", 1)
	}
	add("", "proj-2", "Unknown A", 50)
	add("", "", "Ref-less Company", 70)

	newService := func(cap int) (*ResourceSearch, *membershipOrderedSearcher) {
		searcher := &membershipOrderedSearcher{MockResourceSearcher: mock.NewMockResourceSearcher(), records: records}
		config := DefaultConfig()
		config.MaxSummaryRecords = cap
		svc, err := NewResourceSearch(searcher, mock.NewMockAccessControlChecker(), mock.NewMockResourceFilter(), config)
		require.NoError(t, err)
		return svc.(*ResourceSearch), searcher
	}
	criteria := model.MembershipSummaryCriteria{ProjectUID: "scope"}
	whole, _ := newService(constants.MaxSummaryRecordCap)
	uncapped, err := whole.QueryMembershipSummary(membershipContext("test-user"), criteria)
	require.NoError(t, err)
	require.True(t, uncapped.Complete)
	require.Len(t, uncapped.Summaries, 6)

	capped, searcher := newService(constants.MaxPageSize)
	var union []model.MembershipTermSummary
	seen := make(map[[2]string]bool)
	var terms uint64
	for reads := 1; ; reads++ {
		require.LessOrEqual(t, reads, 6, "the cursor walk must terminate")
		result, err := capped.QueryMembershipSummary(membershipContext("test-user"), criteria)
		require.NoError(t, err)
		if reads == 1 {
			require.False(t, result.Complete)
			require.Equal(t, "parent_refs", searcher.criteria[0].SortBy)
			require.Equal(t, "min", searcher.criteria[0].SortMode)
			require.Len(t, result.Summaries, 2)
			// Output is still sorted by name, NOT by the read's org ref.
			require.Equal(t, "org-b", result.Summaries[0].B2BOrgUID)
			require.Equal(t, "org-a", result.Summaries[1].B2BOrgUID)
			require.Equal(t, uint64(2500), result.Summaries[1].TermCount,
				"both company spellings belong to one whole summary")
			require.Equal(t, "Z New Company", result.Summaries[1].CompanyName)
			require.Equal(t, cursor(`["b2b_org:org-b","m-02549"]`), result.SearchAfter)
		}
		for _, summary := range result.Summaries {
			org, project := "uid:"+summary.B2BOrgUID, "uid:"+summary.ProjectUID
			if summary.B2BOrgUID == "" {
				org = "label:" + strings.ToLower(strings.TrimSpace(summary.CompanyName))
			}
			if summary.ProjectUID == "" {
				project = "label:" + strings.ToLower(strings.TrimSpace(summary.ProjectSlug))
			}
			key := [2]string{org, project}
			require.False(t, seen[key], "no organization/project or label pair may repeat across reads: %v", key)
			seen[key] = true
			union = append(union, summary)
		}
		terms += result.TermsTotal
		if result.Complete {
			require.Nil(t, result.SearchAfter)
			break
		}
		require.NotNil(t, result.SearchAfter)
		require.NotEqual(t, criteria.SearchAfter, result.SearchAfter)
		criteria.SearchAfter = result.SearchAfter
	}
	require.Equal(t, uncapped.TermsTotal, terms)
	require.ElementsMatch(t, uncapped.Summaries, union)
}

// membershipOrderedFixture separates the indexed parent refs from the data
// object; no field is added to the public Resource just for a test.
type membershipOrderedFixture struct {
	resource   model.Resource
	parentRefs []string
}

// membershipOrderedSearcher models the requested ascending field sort and
// keyset paging. Unlike a canned page fixture it would split a renamed org
// under sort_name, so a regression to the old read order cannot pass the walk.
type membershipOrderedSearcher struct {
	*mock.MockResourceSearcher
	records  []membershipOrderedFixture
	criteria []model.SearchCriteria
}

func (s *membershipOrderedSearcher) QueryResources(_ context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {
	s.criteria = append(s.criteria, criteria)
	if criteria.SortOrder != "asc" {
		return nil, fmt.Errorf("fixture requires ascending order")
	}
	type hit struct {
		resource model.Resource
		key      string
		missing  bool
	}
	hits := make([]hit, 0, len(s.records))
	for _, record := range s.records {
		h := hit{resource: record.resource}
		switch criteria.SortBy {
		case "parent_refs":
			if criteria.SortMode != "min" {
				return nil, fmt.Errorf("parent ref sort must explicitly request min")
			}
			h.missing = len(record.parentRefs) == 0
			if !h.missing {
				h.key = slices.Min(record.parentRefs)
			}
		case "sort_name":
			h.key = strings.ToLower(record.resource.Data.(map[string]any)["company_name"].(string))
		default:
			return nil, fmt.Errorf("unsupported fixture sort field %q", criteria.SortBy)
		}
		var first any = h.key
		if h.missing {
			first = nil
		}
		encoded, err := json.Marshal([]any{first, h.resource.ID})
		if err != nil {
			return nil, err
		}
		h.resource.SortValues = string(encoded)
		hits = append(hits, h)
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.missing != b.missing {
			if a.missing {
				return 1
			}
			return -1
		}
		if order := strings.Compare(a.key, b.key); order != 0 {
			return order
		}
		return strings.Compare(a.resource.ID, b.resource.ID)
	})
	start := 0
	if criteria.SearchAfter != nil {
		found := false
		for i, h := range hits {
			if h.resource.SortValues == *criteria.SearchAfter {
				start, found = i+1, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("fixture cursor not found")
		}
	}
	end := min(start+criteria.PageSize, len(hits))
	result := &model.SearchResult{}
	for _, h := range hits[start:end] {
		result.Resources = append(result.Resources, h.resource)
	}
	if end-start == criteria.PageSize {
		result.NextSearchAfter = cursor(hits[end-1].resource.SortValues)
	}
	return result, nil
}
