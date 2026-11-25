// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

// MockOrganizationSearcher is a mock implementation of OrganizationSearcher for testing
// This demonstrates how the clean architecture allows easy swapping of implementations
type MockOrganizationSearcher struct {
	organizations []model.Organization
}

// NewMockOrganizationSearcher creates a new mock organization searcher with sample data
func NewMockOrganizationSearcher() *MockOrganizationSearcher {
	return &MockOrganizationSearcher{
		organizations: []model.Organization{
			{
				Name:      "The Linux Foundation",
				Domain:    "linuxfoundation.org",
				Industry:  "Non-Profit",
				Sector:    "Technology",
				Employees: "100-499",
			},
			{
				Name:      "Zyx-42 Quantum Widgets LLC",
				Domain:    "zyx42-quantum-widgets.fake",
				Industry:  "Imaginary Technology",
				Sector:    "Quantum Widget Manufacturing",
				Employees: "847",
			},
			{
				Name:      "Blorbtech Intergalactic Solutions",
				Domain:    "blorbtech-solutions.notreal",
				Industry:  "Space Commerce",
				Sector:    "Intergalactic Consulting",
				Employees: "23-456",
			},
			{
				Name:      "Fizzlebottom & Associates Pty",
				Domain:    "fizzlebottom-associates.example",
				Industry:  "Professional Services",
				Sector:    "Nonsensical Consulting",
				Employees: "12",
			},
			{
				Name:      "Whizbang Doodad Corporation",
				Domain:    "whizbang-doodads.fake",
				Industry:  "Manufacturing",
				Sector:    "Fictional Doodad Production",
				Employees: "999+",
			},
			{
				Name:      "Sproinkel Digital Dynamics",
				Domain:    "sproinkel-digital.test",
				Industry:  "Technology",
				Sector:    "Made-up Digital Solutions",
				Employees: "73",
			},
			{
				Name:      "Flibber-Jib Environmental Corp",
				Domain:    "flibber-jib-env.localhost",
				Industry:  "Environmental",
				Sector:    "Imaginary Green Technology",
				Employees: "156-789",
			},
			{
				Name:      "Quibblesnort Cybersecurity Ltd",
				Domain:    "quibblesnort-cyber.mock",
				Industry:  "Technology",
				Sector:    "Fictional Security Services",
				Employees: "42",
			},
		},
	}
}

// QueryOrganizations implements the OrganizationSearcher interface with mock data
func (m *MockOrganizationSearcher) QueryOrganizations(ctx context.Context, criteria model.OrganizationSearchCriteria) (*model.Organization, error) {
	slog.DebugContext(ctx, "executing mock organization search",
		"name", criteria.Name,
		"domain", criteria.Domain,
	)

	// Search by exact name match (case-insensitive)
	if criteria.Name != nil {
		searchName := strings.ToLower(*criteria.Name)
		for _, org := range m.organizations {
			if strings.ToLower(org.Name) == searchName {
				slog.DebugContext(ctx, "found organization by name", "organization", org.Name)
				return &org, nil
			}
		}
	}

	// Search by exact domain match (case-insensitive)
	if criteria.Domain != nil {
		searchDomain := strings.ToLower(*criteria.Domain)
		for _, org := range m.organizations {
			if strings.ToLower(org.Domain) == searchDomain {
				slog.DebugContext(ctx, "found organization by domain", "organization", org.Name)
				return &org, nil
			}
		}
	}

	// Not found - return appropriate error
	switch {
	case criteria.Name != nil && criteria.Domain != nil:
		return nil, errors.NewNotFound(fmt.Sprintf("organization not found with name '%s' or domain '%s'", *criteria.Name, *criteria.Domain))
	case criteria.Name != nil:
		return nil, errors.NewNotFound(fmt.Sprintf("organization not found with name '%s'", *criteria.Name))
	case criteria.Domain != nil:
		return nil, errors.NewNotFound(fmt.Sprintf("organization not found with domain '%s'", *criteria.Domain))
	}

	return nil, errors.NewValidation("no search criteria provided")
}

// SuggestOrganizations implements the OrganizationSearcher interface with mock suggestions
func (m *MockOrganizationSearcher) SuggestOrganizations(ctx context.Context, criteria model.OrganizationSuggestionCriteria) (*model.OrganizationSuggestionsResult, error) {
	slog.DebugContext(ctx, "executing mock organization suggestions search",
		"query", criteria.Query,
	)

	suggestions := make([]model.OrganizationSuggestion, 0)
	query := strings.ToLower(strings.TrimSpace(criteria.Query))

	// Search for organizations that match the query (case-insensitive partial match)
	for _, org := range m.organizations {
		if strings.Contains(strings.ToLower(org.Name), query) || strings.Contains(strings.ToLower(org.Domain), query) {
			suggestions = append(suggestions, model.OrganizationSuggestion{
				Name:   org.Name,
				Domain: org.Domain,
				Logo:   nil, // Mock doesn't have logo data
			})
		}
	}

	// Limit to first 5 suggestions for realistic behavior
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	result := &model.OrganizationSuggestionsResult{
		Suggestions: suggestions,
	}

	slog.DebugContext(ctx, "mock organization suggestions search completed",
		"query", criteria.Query,
		"suggestion_count", len(suggestions),
	)

	return result, nil
}

// IsReady implements the OrganizationSearcher interface (always ready for mock)
func (m *MockOrganizationSearcher) IsReady(ctx context.Context) error {
	return nil
}

// AddOrganization adds an organization to the mock data (useful for testing)
func (m *MockOrganizationSearcher) AddOrganization(org model.Organization) {
	m.organizations = append(m.organizations, org)
}

// ClearOrganizations clears all organizations (useful for testing)
func (m *MockOrganizationSearcher) ClearOrganizations() {
	m.organizations = []model.Organization{}
}

// GetOrganizationCount returns the total number of organizations
func (m *MockOrganizationSearcher) GetOrganizationCount() int {
	return len(m.organizations)
}

// GetOrganizationByName returns an organization by name (for testing purposes)
func (m *MockOrganizationSearcher) GetOrganizationByName(name string) *model.Organization {
	searchName := strings.ToLower(name)
	for _, org := range m.organizations {
		if strings.ToLower(org.Name) == searchName {
			return &org
		}
	}
	return nil
}

// GetOrganizationByDomain returns an organization by domain (for testing purposes)
func (m *MockOrganizationSearcher) GetOrganizationByDomain(domain string) *model.Organization {
	searchDomain := strings.ToLower(domain)
	for _, org := range m.organizations {
		if strings.ToLower(org.Domain) == searchDomain {
			return &org
		}
	}
	return nil
}

// GetAllOrganizations returns all organizations (for testing purposes)
func (m *MockOrganizationSearcher) GetAllOrganizations() []model.Organization {
	return m.organizations
}
