// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

func TestFoldMembershipTerms(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rows     []map[string]any
		expected []model.MembershipTermSummary
	}{
		{
			name:     "no records fold into no summaries",
			rows:     nil,
			expected: []model.MembershipTermSummary{},
		},
		{
			name: "records read on several pages fold into one summary per organization and project",
			rows: []map[string]any{
				// First page.
				{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
				{
					"uid": "m-3", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-2", "project_slug": "other-project",
					"status": "Active", "tier_name": "Platinum", "tier": "Platinum Member",
					"start_date": "2024-06-01T00:00:00Z", "end_date": "2025-06-01T00:00:00Z",
					"created_at": "2024-06-02T00:00:00Z",
				},
				// Second page.
				{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold", "tier": "Gold Member",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Expired", "Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold", Tier: "Gold Member",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-2", ProjectSlug: "other-project",
					TermCount:  1,
					FirstStart: "2024-06-01T00:00:00Z", LastEnd: "2025-06-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Platinum",
					CurrentStart: "2024-06-01T00:00:00Z", CurrentEnd: "2025-06-01T00:00:00Z",
					CurrentMembershipUID: "m-3",
					TierNames:            []string{"Platinum"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-3", Status: "Active", TierName: "Platinum", Tier: "Platinum Member",
							StartDate: "2024-06-01T00:00:00Z", EndDate: "2025-06-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "the active record is current even when another record starts later",
			rows: []map[string]any{
				{
					"uid": "m-late", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Invoice Cancelled", "tier_name": "Gold",
					"start_date": "2026-01-01T00:00:00Z", "end_date": "2027-01-01T00:00:00Z",
					"created_at": "2025-12-01T00:00:00Z",
				},
				{
					"uid": "m-active", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Silver",
					"start_date": "2025-01-01T00:00:00Z", "end_date": "2026-01-01T00:00:00Z",
					"created_at": "2024-12-01T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2025-01-01T00:00:00Z", LastEnd: "2027-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Silver",
					CurrentStart: "2025-01-01T00:00:00Z", CurrentEnd: "2026-01-01T00:00:00Z",
					CurrentMembershipUID: "m-active",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Active", "Invoice Cancelled"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-active", Status: "Active", TierName: "Silver",
							StartDate: "2025-01-01T00:00:00Z", EndDate: "2026-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-late", Status: "Invoice Cancelled", TierName: "Gold",
							StartDate: "2026-01-01T00:00:00Z", EndDate: "2027-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "the latest starting active record is current when several are active",
			rows: []map[string]any{
				{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
				{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Active", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "the latest starting record is current when no record is active",
			rows: []map[string]any{
				{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Expired", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Expired"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Expired", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records without dates leave the first start and last end of the records that have them",
			rows: []map[string]any{
				{
					"uid": "m-open", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-undated", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Purchased", "tier_name": "Gold",
					"created_at": "2022-01-02T00:00:00Z",
				},
				{
					"uid": "m-closed", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  3,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2024-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "",
					CurrentMembershipUID: "m-open",
					TierNames:            []string{"Gold", "Silver"},
					Statuses:             []string{"Purchased", "Expired", "Active"},
					Terms: []model.MembershipTerm{
						{MembershipUID: "m-undated", Status: "Purchased", TierName: "Gold"},
						{
							MembershipUID: "m-closed", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-open", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records without an organization UID fold under their company name",
			rows: []map[string]any{
				{
					"uid": "m-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
				{
					"uid": "m-2", "company_name": " example corp ",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "", CompanyName: " example corp ",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Expired", "Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records without a project UID fold under their project slug",
			rows: []map[string]any{
				{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_slug": "legacy-project",
					"status":       "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
				{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_slug": "Legacy-Project",
					"status":       "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-3", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "onboarded-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "", ProjectSlug: "Legacy-Project",
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Expired", "Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "onboarded-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-3",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-3", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records without any identifier or label fold under the empty key",
			rows: []map[string]any{
				{
					"uid": "m-1", "status": "Expired", "tier_name": "Silver",
					"start_date": "2023-01-01T00:00:00Z", "end_date": "2024-01-01T00:00:00Z",
					"created_at": "2023-01-02T00:00:00Z",
				},
				{
					"uid": "m-2", "status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					TermCount:  2,
					FirstStart: "2023-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Silver", "Gold"},
					Statuses:             []string{"Expired", "Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Expired", TierName: "Silver",
							StartDate: "2023-01-01T00:00:00Z", EndDate: "2024-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records covering the same dates stay separate terms",
			rows: []map[string]any{
				{
					"uid": "m-1", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-2", "b2b_org_uid": "org-1", "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  2,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-1", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
						{
							MembershipUID: "m-2", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "summaries are ordered by company name, project slug and the two UIDs",
			rows: []map[string]any{
				{
					"uid": "m-zeta", "b2b_org_uid": "org-2", "company_name": "Zeta Ltd",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-alpha-b", "b2b_org_uid": "org-1", "company_name": "Alpha Inc",
					"project_uid": "proj-3", "project_slug": "second-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
				{
					"uid": "m-alpha-a", "b2b_org_uid": "org-1", "company_name": "Alpha Inc",
					"project_uid": "proj-2", "project_slug": "first-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
					"created_at": "2024-01-02T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "org-1", CompanyName: "Alpha Inc",
					ProjectUID: "proj-2", ProjectSlug: "first-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-alpha-a",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-alpha-a", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
				{
					B2BOrgUID: "org-1", CompanyName: "Alpha Inc",
					ProjectUID: "proj-3", ProjectSlug: "second-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-alpha-b",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-alpha-b", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
				{
					B2BOrgUID: "org-2", CompanyName: "Zeta Ltd",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-zeta",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							MembershipUID: "m-zeta", Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "records whose fields are not stored as text fold with empty values",
			rows: []map[string]any{
				{
					"uid": 42, "b2b_org_uid": nil, "company_name": "Example Corp",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "", CompanyName: "Example Corp",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "",
					TierNames:            []string{"Gold"},
					Statuses:             []string{"Active"},
					Terms: []model.MembershipTerm{
						{
							Status: "Active", TierName: "Gold",
							StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
						},
					},
				},
			},
		},
		{
			name: "a UID and a label that spell the same keep separate summaries",
			rows: []map[string]any{
				{
					"uid": "m-1", "b2b_org_uid": "acme", "company_name": "Acme Ltd",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
				},
				{
					"uid": "m-2", "company_name": "acme",
					"project_uid": "proj-1", "project_slug": "example-project",
					"status": "Active", "tier_name": "Gold",
					"start_date": "2024-01-01T00:00:00Z", "end_date": "2025-01-01T00:00:00Z",
				},
			},
			expected: []model.MembershipTermSummary{
				{
					B2BOrgUID: "acme", CompanyName: "Acme Ltd",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-1",
					TierNames:            []string{"Gold"}, Statuses: []string{"Active"},
					Terms: []model.MembershipTerm{{
						MembershipUID: "m-1", Status: "Active", TierName: "Gold",
						StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
					}},
				},
				{
					B2BOrgUID: "", CompanyName: "acme",
					ProjectUID: "proj-1", ProjectSlug: "example-project",
					TermCount:  1,
					FirstStart: "2024-01-01T00:00:00Z", LastEnd: "2025-01-01T00:00:00Z",
					CurrentStatus: "Active", CurrentTierName: "Gold",
					CurrentStart: "2024-01-01T00:00:00Z", CurrentEnd: "2025-01-01T00:00:00Z",
					CurrentMembershipUID: "m-2",
					TierNames:            []string{"Gold"}, Statuses: []string{"Active"},
					Terms: []model.MembershipTerm{{
						MembershipUID: "m-2", Status: "Active", TierName: "Gold",
						StartDate: "2024-01-01T00:00:00Z", EndDate: "2025-01-01T00:00:00Z",
					}},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summaries := foldMembershipTerms(tc.rows)
			require.Equal(t, tc.expected, summaries)

			folded := 0
			for _, summary := range summaries {
				folded += len(summary.Terms)
				require.Equal(t, uint64(len(summary.Terms)), summary.TermCount)
			}
			require.Equal(t, len(tc.rows), folded, "every record must be folded into a summary")
		})
	}
}
