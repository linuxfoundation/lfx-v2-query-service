// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"testing"

	server "github.com/linuxfoundation/lfx-v2-query-service/gen/http/query_svc/server"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestPayloadToCriteria(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	// Setup environment variable for page token secret
	t.Setenv("PAGE_TOKEN_SECRET", "12345678901234567890123456789012") // 32 chars

	tests := []struct {
		name             string
		payload          *querysvc.QueryResourcesPayload
		expectedCriteria model.SearchCriteria
		expectedError    bool
	}{
		{
			name: "basic payload conversion",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test-project"),
				Type:     stringPtr("project"),
				Tags:     []string{"active", "governance"},
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:         stringPtr("test-project"),
				ResourceType: stringPtr("project"),
				Tags:         []string{"active", "governance"},
				PageSize:     constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with parent",
			payload: &querysvc.QueryResourcesPayload{
				Parent:   stringPtr("parent-id"),
				Name:     stringPtr("child-resource"),
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:     stringPtr("child-resource"),
				Parent:   stringPtr("parent-id"),
				PageSize: constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with sorting - name_asc",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				Sort:     "name_asc",
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:      stringPtr("test"),
				SortBy:    "sort_name",
				SortOrder: "asc",
				PageSize:  constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with sorting - name_desc",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				Sort:     "name_desc",
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:      stringPtr("test"),
				SortBy:    "sort_name",
				SortOrder: "desc",
				PageSize:  constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with sorting - updated_asc",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				Sort:     "updated_asc",
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:      stringPtr("test"),
				SortBy:    "updated_at",
				SortOrder: "asc",
				PageSize:  constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with sorting - updated_desc",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				Sort:     "updated_desc",
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:      stringPtr("test"),
				SortBy:    "updated_at",
				SortOrder: "desc",
				PageSize:  constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with sorting - best_match",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				Sort:     "best_match",
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				Name:      stringPtr("test"),
				SortBy:    "_score",
				SortOrder: "desc",
				PageSize:  constants.DefaultPageSize,
			},
			expectedError: false,
		},
		{
			name: "payload with explicit page_size",
			payload: &querysvc.QueryResourcesPayload{
				Name:     stringPtr("test"),
				PageSize: 20,
			},
			expectedCriteria: model.SearchCriteria{
				Name:     stringPtr("test"),
				PageSize: 20,
			},
			expectedError: false,
		},
		{
			name: "payload with invalid page token",
			payload: &querysvc.QueryResourcesPayload{
				Name:      stringPtr("test"),
				PageToken: stringPtr("invalid-token"),
				PageSize:  constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{}, // Will be empty due to error
			expectedError:    true,
		},
		{
			name: "empty payload",
			payload: &querysvc.QueryResourcesPayload{
				PageSize: constants.DefaultPageSize,
			},
			expectedCriteria: model.SearchCriteria{
				PageSize: constants.DefaultPageSize,
			},
			expectedError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Execute
			result, err := svc.payloadToCriteria(ctx, tc.payload)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedCriteria.Name, result.Name)
				assert.Equal(t, tc.expectedCriteria.Parent, result.Parent)
				assert.Equal(t, tc.expectedCriteria.ResourceType, result.ResourceType)
				assert.Equal(t, tc.expectedCriteria.Tags, result.Tags)
				assert.Equal(t, tc.expectedCriteria.SortBy, result.SortBy)
				assert.Equal(t, tc.expectedCriteria.SortOrder, result.SortOrder)
				assert.Equal(t, tc.expectedCriteria.PageSize, result.PageSize)
			}
		})
	}
}

func TestDomainResultToResponse(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name             string
		domainResult     *model.SearchResult
		expectedResponse *querysvc.QueryResourcesResult
	}{
		{
			name: "basic domain result conversion",
			domainResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "test-project-1",
						Data: map[string]any{
							"name":        "Test Project 1",
							"description": "A test project",
						},
					},
					{
						Type: "organization",
						ID:   "test-org-1",
						Data: map[string]any{
							"name": "Test Organization",
						},
					},
				},
				PageToken:    stringPtr("next-page-token"),
				CacheControl: stringPtr("public, max-age=300"),
				Total:        2,
			},
			expectedResponse: &querysvc.QueryResourcesResult{
				Resources: []*querysvc.Resource{
					{
						Type: stringPtr("project"),
						ID:   stringPtr("test-project-1"),
						Data: map[string]any{
							"name":        "Test Project 1",
							"description": "A test project",
						},
					},
					{
						Type: stringPtr("organization"),
						ID:   stringPtr("test-org-1"),
						Data: map[string]any{
							"name": "Test Organization",
						},
					},
				},
				PageToken:    stringPtr("next-page-token"),
				CacheControl: stringPtr("public, max-age=300"),
			},
		},
		{
			name: "empty domain result",
			domainResult: &model.SearchResult{
				Resources:    []model.Resource{},
				PageToken:    nil,
				CacheControl: nil,
				Total:        0,
			},
			expectedResponse: &querysvc.QueryResourcesResult{
				Resources:    []*querysvc.Resource{},
				PageToken:    nil,
				CacheControl: nil,
			},
		},
		{
			name: "single resource result",
			domainResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "single-project",
						Data: map[string]any{
							"name": "Single Project",
						},
					},
				},
				Total: 1,
			},
			expectedResponse: &querysvc.QueryResourcesResult{
				Resources: []*querysvc.Resource{
					{
						Type: stringPtr("project"),
						ID:   stringPtr("single-project"),
						Data: map[string]any{
							"name": "Single Project",
						},
					},
				},
				PageToken:    nil,
				CacheControl: nil,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			result := svc.domainResultToResponse(tc.domainResult)

			// Verify
			assert.NotNil(t, result)
			assert.Equal(t, len(tc.expectedResponse.Resources), len(result.Resources))

			for i, expectedResource := range tc.expectedResponse.Resources {
				assert.Equal(t, expectedResource.Type, result.Resources[i].Type)
				assert.Equal(t, expectedResource.ID, result.Resources[i].ID)
				assert.Equal(t, expectedResource.Data, result.Resources[i].Data)
			}

			assert.Equal(t, tc.expectedResponse.PageToken, result.PageToken)
			assert.Equal(t, tc.expectedResponse.CacheControl, result.CacheControl)
		})
	}
}

func TestPayloadToOrganizationCriteria(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name             string
		payload          *querysvc.QueryOrgsPayload
		expectedCriteria model.OrganizationSearchCriteria
	}{
		{
			name: "payload with name only",
			payload: &querysvc.QueryOrgsPayload{
				Name: stringPtr("The Linux Foundation"),
			},
			expectedCriteria: model.OrganizationSearchCriteria{
				Name: stringPtr("The Linux Foundation"),
			},
		},
		{
			name: "payload with domain only",
			payload: &querysvc.QueryOrgsPayload{
				Domain: stringPtr("linuxfoundation.org"),
			},
			expectedCriteria: model.OrganizationSearchCriteria{
				Domain: stringPtr("linuxfoundation.org"),
			},
		},
		{
			name: "payload with both name and domain",
			payload: &querysvc.QueryOrgsPayload{
				Name:   stringPtr("The Linux Foundation"),
				Domain: stringPtr("linuxfoundation.org"),
			},
			expectedCriteria: model.OrganizationSearchCriteria{
				Name:   stringPtr("The Linux Foundation"),
				Domain: stringPtr("linuxfoundation.org"),
			},
		},
		{
			name:    "empty payload",
			payload: &querysvc.QueryOrgsPayload{},
			expectedCriteria: model.OrganizationSearchCriteria{
				Name:   nil,
				Domain: nil,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Execute
			result := svc.payloadToOrganizationCriteria(ctx, tc.payload)

			// Verify
			assert.Equal(t, tc.expectedCriteria.Name, result.Name)
			assert.Equal(t, tc.expectedCriteria.Domain, result.Domain)
		})
	}
}

func TestDomainOrganizationToResponse(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name             string
		domainOrg        *model.Organization
		expectedResponse *querysvc.Organization
	}{
		{
			name: "complete organization conversion",
			domainOrg: &model.Organization{
				Name:      "The Linux Foundation",
				Domain:    "linuxfoundation.org",
				Industry:  "Non-Profit",
				Sector:    "Technology",
				Employees: "100-499",
			},
			expectedResponse: &querysvc.Organization{
				Name:      stringPtr("The Linux Foundation"),
				Domain:    stringPtr("linuxfoundation.org"),
				Industry:  stringPtr("Non-Profit"),
				Sector:    stringPtr("Technology"),
				Employees: stringPtr("100-499"),
			},
		},
		{
			name: "minimal organization conversion",
			domainOrg: &model.Organization{
				Name:   "Test Org",
				Domain: "test.org",
			},
			expectedResponse: &querysvc.Organization{
				Name:      stringPtr("Test Org"),
				Domain:    stringPtr("test.org"),
				Industry:  stringPtr(""),
				Sector:    stringPtr(""),
				Employees: stringPtr(""),
			},
		},
		{
			name: "organization with empty fields",
			domainOrg: &model.Organization{
				Name:      "Empty Fields Org",
				Domain:    "empty.org",
				Industry:  "",
				Sector:    "",
				Employees: "",
			},
			expectedResponse: &querysvc.Organization{
				Name:      stringPtr("Empty Fields Org"),
				Domain:    stringPtr("empty.org"),
				Industry:  stringPtr(""),
				Sector:    stringPtr(""),
				Employees: stringPtr(""),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			result := svc.domainOrganizationToResponse(tc.domainOrg)

			// Verify
			assert.NotNil(t, result)
			assert.Equal(t, tc.expectedResponse.Name, result.Name)
			assert.Equal(t, tc.expectedResponse.Domain, result.Domain)
			assert.Equal(t, tc.expectedResponse.Industry, result.Industry)
			assert.Equal(t, tc.expectedResponse.Sector, result.Sector)
			assert.Equal(t, tc.expectedResponse.Employees, result.Employees)
		})
	}
}

func TestPayloadToOrganizationSuggestionCriteria(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name             string
		payload          *querysvc.SuggestOrgsPayload
		expectedCriteria model.OrganizationSuggestionCriteria
	}{
		{
			name: "payload with query",
			payload: &querysvc.SuggestOrgsPayload{
				Query: "linux",
			},
			expectedCriteria: model.OrganizationSuggestionCriteria{
				Query: "linux",
			},
		},
		{
			name: "payload with empty query",
			payload: &querysvc.SuggestOrgsPayload{
				Query: "",
			},
			expectedCriteria: model.OrganizationSuggestionCriteria{
				Query: "",
			},
		},
		{
			name: "payload with complex query",
			payload: &querysvc.SuggestOrgsPayload{
				Query: "linux foundation open source",
			},
			expectedCriteria: model.OrganizationSuggestionCriteria{
				Query: "linux foundation open source",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Execute
			result := svc.payloadToOrganizationSuggestionCriteria(ctx, tc.payload)

			// Verify
			assert.Equal(t, tc.expectedCriteria.Query, result.Query)
		})
	}
}

func TestDomainOrganizationSuggestionsToResponse(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name             string
		domainResult     *model.OrganizationSuggestionsResult
		expectedResponse *querysvc.SuggestOrgsResult
	}{
		{
			name: "suggestions with results",
			domainResult: &model.OrganizationSuggestionsResult{
				Suggestions: []model.OrganizationSuggestion{
					{
						Name:   "The Linux Foundation",
						Domain: "linuxfoundation.org",
						Logo:   stringPtr("https://example.com/logo1.png"),
					},
					{
						Name:   "Linux Kernel Organization",
						Domain: "kernel.org",
						Logo:   stringPtr("https://example.com/logo2.png"),
					},
				},
			},
			expectedResponse: &querysvc.SuggestOrgsResult{
				Suggestions: []*querysvc.OrganizationSuggestion{
					{
						Name:   "The Linux Foundation",
						Domain: "linuxfoundation.org",
						Logo:   stringPtr("https://example.com/logo1.png"),
					},
					{
						Name:   "Linux Kernel Organization",
						Domain: "kernel.org",
						Logo:   stringPtr("https://example.com/logo2.png"),
					},
				},
			},
		},
		{
			name: "empty suggestions",
			domainResult: &model.OrganizationSuggestionsResult{
				Suggestions: []model.OrganizationSuggestion{},
			},
			expectedResponse: &querysvc.SuggestOrgsResult{
				Suggestions: []*querysvc.OrganizationSuggestion{},
			},
		},
		{
			name:         "nil domain result",
			domainResult: nil,
			expectedResponse: &querysvc.SuggestOrgsResult{
				Suggestions: []*querysvc.OrganizationSuggestion{},
			},
		},
		{
			name: "suggestions with partial data",
			domainResult: &model.OrganizationSuggestionsResult{
				Suggestions: []model.OrganizationSuggestion{
					{
						Name:   "Test Org",
						Domain: "test.org",
						Logo:   nil, // Logo is nil
					},
				},
			},
			expectedResponse: &querysvc.SuggestOrgsResult{
				Suggestions: []*querysvc.OrganizationSuggestion{
					{
						Name:   "Test Org",
						Domain: "test.org",
						Logo:   nil,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			result := svc.domainOrganizationSuggestionsToResponse(tc.domainResult)

			// Verify
			assert.NotNil(t, result)
			assert.NotNil(t, result.Suggestions)
			assert.Equal(t, len(tc.expectedResponse.Suggestions), len(result.Suggestions))

			for i, expectedSuggestion := range tc.expectedResponse.Suggestions {
				assert.Equal(t, expectedSuggestion.Name, result.Suggestions[i].Name)
				assert.Equal(t, expectedSuggestion.Domain, result.Suggestions[i].Domain)
				assert.Equal(t, expectedSuggestion.Logo, result.Suggestions[i].Logo)
			}
		})
	}
}

func TestParseDateFilter(t *testing.T) {
	tests := []struct {
		name        string
		dateStr     string
		isEndDate   bool
		expected    string
		expectError bool
	}{
		{
			name:        "ISO 8601 datetime format (start date)",
			dateStr:     "2025-01-10T15:30:00Z",
			isEndDate:   false,
			expected:    "2025-01-10T15:30:00Z",
			expectError: false,
		},
		{
			name:        "ISO 8601 datetime format (end date)",
			dateStr:     "2025-01-28T23:59:59Z",
			isEndDate:   true,
			expected:    "2025-01-28T23:59:59Z",
			expectError: false,
		},
		{
			name:        "date-only format converted to start of day",
			dateStr:     "2025-01-10",
			isEndDate:   false,
			expected:    "2025-01-10T00:00:00Z",
			expectError: false,
		},
		{
			name:        "date-only format converted to end of day",
			dateStr:     "2025-01-28",
			isEndDate:   true,
			expected:    "2025-01-28T23:59:59Z",
			expectError: false,
		},
		{
			name:        "empty string returns empty",
			dateStr:     "",
			isEndDate:   false,
			expected:    "",
			expectError: false,
		},
		{
			name:        "invalid date format",
			dateStr:     "01/10/2025",
			isEndDate:   false,
			expected:    "",
			expectError: true,
		},
		{
			name:        "invalid date string",
			dateStr:     "not-a-date",
			isEndDate:   false,
			expected:    "",
			expectError: true,
		},
		{
			name:        "partial datetime (missing timezone)",
			dateStr:     "2025-01-10T15:30:00",
			isEndDate:   false,
			expected:    "",
			expectError: true,
		},
		{
			name:        "date-only with different year",
			dateStr:     "2024-12-31",
			isEndDate:   true,
			expected:    "2024-12-31T23:59:59Z",
			expectError: false,
		},
		{
			name:        "ISO 8601 with milliseconds (truncated to seconds)",
			dateStr:     "2025-01-10T15:30:00.123Z",
			isEndDate:   false,
			expected:    "2025-01-10T15:30:00Z",
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			result, err := parseDateFilter(tc.dateStr, tc.isEndDate)

			// Verify
			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid date format")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name           string
		filters        []string
		expected       []model.FieldFilter
		expectedError  bool
		errorSubstring string
	}{
		{
			name:          "valid single filter - auto-prefixed with data",
			filters:       []string{"status:active"},
			expected:      []model.FieldFilter{{Field: "data.status", Value: "active"}},
			expectedError: false,
		},
		{
			name:    "valid multiple filters - auto-prefixed with data",
			filters: []string{"status:active", "priority:high"},
			expected: []model.FieldFilter{
				{Field: "data.status", Value: "active"},
				{Field: "data.priority", Value: "high"},
			},
			expectedError: false,
		},
		{
			name:          "filter with spaces (trimmed and auto-prefixed)",
			filters:       []string{" status : active "},
			expected:      []model.FieldFilter{{Field: "data.status", Value: "active"}},
			expectedError: false,
		},
		{
			name:          "filter with colon in value",
			filters:       []string{"url:https://example.com"},
			expected:      []model.FieldFilter{{Field: "data.url", Value: "https://example.com"}},
			expectedError: false,
		},
		{
			name:           "invalid filter format (no colon)",
			filters:        []string{"invalid"},
			expected:       nil,
			expectedError:  true,
			errorSubstring: "invalid filter format",
		},
		{
			name:          "invalid filter format (empty after colon)",
			filters:       []string{"status:"},
			expected:      []model.FieldFilter{{Field: "data.status", Value: ""}},
			expectedError: false,
		},
		{
			name:           "invalid filter format (empty field name)",
			filters:        []string{":value"},
			expected:       nil,
			expectedError:  true,
			errorSubstring: "field name cannot be empty",
		},
		{
			name:           "invalid filter format (whitespace-only field name)",
			filters:        []string{"  :value"},
			expected:       nil,
			expectedError:  true,
			errorSubstring: "field name cannot be empty",
		},
		{
			name:          "empty filters array",
			filters:       []string{},
			expected:      nil,
			expectedError: false,
		},
		{
			name:          "nil filters",
			filters:       nil,
			expected:      nil,
			expectedError: false,
		},
		{
			name:          "nested field name (auto-prefixed)",
			filters:       []string{"project.id:123"},
			expected:      []model.FieldFilter{{Field: "data.project.id", Value: "123"}},
			expectedError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseFilters(tc.filters)

			if tc.expectedError {
				assert.Error(t, err)
				if tc.errorSubstring != "" {
					assert.Contains(t, err.Error(), tc.errorSubstring)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestPayloadToCriteriaWithDateFilters(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name          string
		payload       *querysvc.QueryResourcesPayload
		expectError   bool
		checkCriteria func(*testing.T, model.SearchCriteria)
	}{
		{
			name: "date range with ISO 8601 format",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("updated_at"),
				DateFrom:  stringPtr("2025-01-10T00:00:00Z"),
				DateTo:    stringPtr("2025-01-28T23:59:59Z"),
			},
			expectError: false,
			checkCriteria: func(t *testing.T, c model.SearchCriteria) {
				assert.NotNil(t, c.DateField)
				assert.Equal(t, "data.updated_at", *c.DateField)
				assert.NotNil(t, c.DateFrom)
				assert.Equal(t, "2025-01-10T00:00:00Z", *c.DateFrom)
				assert.NotNil(t, c.DateTo)
				assert.Equal(t, "2025-01-28T23:59:59Z", *c.DateTo)
			},
		},
		{
			name: "date range with date-only format",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("created_at"),
				DateFrom:  stringPtr("2025-01-10"),
				DateTo:    stringPtr("2025-01-28"),
			},
			expectError: false,
			checkCriteria: func(t *testing.T, c model.SearchCriteria) {
				assert.NotNil(t, c.DateField)
				assert.Equal(t, "data.created_at", *c.DateField)
				assert.NotNil(t, c.DateFrom)
				assert.Equal(t, "2025-01-10T00:00:00Z", *c.DateFrom)
				assert.NotNil(t, c.DateTo)
				assert.Equal(t, "2025-01-28T23:59:59Z", *c.DateTo)
			},
		},
		{
			name: "date range with only date_from",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("updated_at"),
				DateFrom:  stringPtr("2025-01-10"),
			},
			expectError: false,
			checkCriteria: func(t *testing.T, c model.SearchCriteria) {
				assert.NotNil(t, c.DateField)
				assert.Equal(t, "data.updated_at", *c.DateField)
				assert.NotNil(t, c.DateFrom)
				assert.Equal(t, "2025-01-10T00:00:00Z", *c.DateFrom)
				assert.Nil(t, c.DateTo)
			},
		},
		{
			name: "date range with only date_to",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("updated_at"),
				DateTo:    stringPtr("2025-01-28"),
			},
			expectError: false,
			checkCriteria: func(t *testing.T, c model.SearchCriteria) {
				assert.NotNil(t, c.DateField)
				assert.Equal(t, "data.updated_at", *c.DateField)
				assert.Nil(t, c.DateFrom)
				assert.NotNil(t, c.DateTo)
				assert.Equal(t, "2025-01-28T23:59:59Z", *c.DateTo)
			},
		},
		{
			name: "invalid date_from format",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("updated_at"),
				DateFrom:  stringPtr("invalid-date"),
			},
			expectError: true,
		},
		{
			name: "invalid date_to format",
			payload: &querysvc.QueryResourcesPayload{
				DateField: stringPtr("updated_at"),
				DateTo:    stringPtr("01/28/2025"),
			},
			expectError: true,
		},
		{
			name: "no date filtering (nil date_field)",
			payload: &querysvc.QueryResourcesPayload{
				Name: stringPtr("test"),
			},
			expectError: false,
			checkCriteria: func(t *testing.T, c model.SearchCriteria) {
				assert.Nil(t, c.DateField)
				assert.Nil(t, c.DateFrom)
				assert.Nil(t, c.DateTo)
			},
		},
		{
			name: "date_from without date_field (should error)",
			payload: &querysvc.QueryResourcesPayload{
				DateFrom: stringPtr("2025-01-10"),
			},
			expectError: true,
		},
		{
			name: "date_to without date_field (should error)",
			payload: &querysvc.QueryResourcesPayload{
				DateTo: stringPtr("2025-01-28"),
			},
			expectError: true,
		},
		{
			name: "date_from and date_to without date_field (should error)",
			payload: &querysvc.QueryResourcesPayload{
				DateFrom: stringPtr("2025-01-10"),
				DateTo:   stringPtr("2025-01-28"),
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Execute
			result, err := svc.payloadToCriteria(ctx, tc.payload)

			// Verify
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tc.checkCriteria != nil {
					tc.checkCriteria(t, result)
				}
			}
		})
	}
}

func TestApplyCommonFields(t *testing.T) {
	tests := []struct {
		name          string
		params        commonQueryParams
		seed          model.SearchCriteria // pre-populated caller fields
		expectError   bool
		errorContains string
		check         func(*testing.T, model.SearchCriteria)
	}{
		{
			name: "sets name, parent, type",
			params: commonQueryParams{
				Name:   stringPtr("my-resource"),
				Parent: stringPtr("parent-id"),
				Type:   stringPtr("project"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("my-resource"), c.Name)
				assert.Equal(t, stringPtr("parent-id"), c.Parent)
				assert.Equal(t, stringPtr("project"), c.ResourceType)
			},
		},
		{
			name: "parses filters and tags",
			params: commonQueryParams{
				Tags:       []string{"active"},
				TagsAll:    []string{"governance"},
				Filters:    []string{"status:active"},
				FiltersAll: []string{"priority:high"},
				FiltersOr:  []string{"region:us"},
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, []string{"active"}, c.Tags)
				assert.Equal(t, []string{"governance"}, c.TagsAll)
				assert.Equal(t, []model.FieldFilter{{Field: "data.status", Value: "active"}}, c.Filters)
				assert.Equal(t, []model.FieldFilter{{Field: "data.priority", Value: "high"}}, c.FiltersAll)
				assert.Equal(t, []model.FieldFilter{{Field: "data.region", Value: "us"}}, c.FiltersOr)
			},
		},
		{
			name: "date range with ISO 8601",
			params: commonQueryParams{
				DateField: stringPtr("updated_at"),
				DateFrom:  stringPtr("2025-01-10T00:00:00Z"),
				DateTo:    stringPtr("2025-01-28T23:59:59Z"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("data.updated_at"), c.DateField)
				assert.Equal(t, stringPtr("2025-01-10T00:00:00Z"), c.DateFrom)
				assert.Equal(t, stringPtr("2025-01-28T23:59:59Z"), c.DateTo)
			},
		},
		{
			name: "date range with date-only format",
			params: commonQueryParams{
				DateField: stringPtr("created_at"),
				DateFrom:  stringPtr("2025-01-10"),
				DateTo:    stringPtr("2025-01-28"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("data.created_at"), c.DateField)
				assert.Equal(t, stringPtr("2025-01-10T00:00:00Z"), c.DateFrom)
				assert.Equal(t, stringPtr("2025-01-28T23:59:59Z"), c.DateTo)
			},
		},
		{
			name: "date_field only (no date_from or date_to)",
			params: commonQueryParams{
				DateField: stringPtr("updated_at"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("data.updated_at"), c.DateField)
				assert.Nil(t, c.DateFrom)
				assert.Nil(t, c.DateTo)
			},
		},
		{
			name:          "date_from without date_field returns error",
			params:        commonQueryParams{DateFrom: stringPtr("2025-01-10")},
			expectError:   true,
			errorContains: "date_field is required",
		},
		{
			name:          "date_to without date_field returns error",
			params:        commonQueryParams{DateTo: stringPtr("2025-01-28")},
			expectError:   true,
			errorContains: "date_field is required",
		},
		{
			name:          "invalid filter format returns error",
			params:        commonQueryParams{Filters: []string{"no-colon"}},
			expectError:   true,
			errorContains: "invalid filter",
		},
		{
			name:          "invalid date_from returns error",
			params:        commonQueryParams{DateField: stringPtr("f"), DateFrom: stringPtr("not-a-date")},
			expectError:   true,
			errorContains: "invalid date_from",
		},
		{
			name:          "invalid date_to returns error",
			params:        commonQueryParams{DateField: stringPtr("f"), DateTo: stringPtr("01/28/2025")},
			expectError:   true,
			errorContains: "invalid date_to",
		},
		{
			name:   "preserves caller-set fields in seed criteria",
			params: commonQueryParams{},
			seed:   model.SearchCriteria{PageSize: 42, PublicOnly: true},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, 42, c.PageSize)
				assert.True(t, c.PublicOnly)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			criteria := tc.seed
			err := applyCommonFields(&criteria, tc.params)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}
			assert.NoError(t, err)
			if tc.check != nil {
				tc.check(t, criteria)
			}
		})
	}
}

func TestPayloadToCountPublicCriteria(t *testing.T) {
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name          string
		payload       *querysvc.QueryResourcesCountPayload
		expectError   bool
		errorContains string
		check         func(*testing.T, model.SearchCriteria)
	}{
		{
			name:    "empty payload sets public-only defaults",
			payload: &querysvc.QueryResourcesCountPayload{},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.True(t, c.PublicOnly)
				assert.False(t, c.PrivateOnly)
				assert.Equal(t, -1, c.PageSize)
			},
		},
		{
			name: "sets name, parent, type from payload",
			payload: &querysvc.QueryResourcesCountPayload{
				Name:   stringPtr("my-project"),
				Parent: stringPtr("p1"),
				Type:   stringPtr("project"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("my-project"), c.Name)
				assert.Equal(t, stringPtr("p1"), c.Parent)
				assert.Equal(t, stringPtr("project"), c.ResourceType)
			},
		},
		{
			name: "date range is applied",
			payload: &querysvc.QueryResourcesCountPayload{
				DateField: stringPtr("created_at"),
				DateFrom:  stringPtr("2025-01-01"),
				DateTo:    stringPtr("2025-12-31"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("data.created_at"), c.DateField)
				assert.NotNil(t, c.DateFrom)
				assert.NotNil(t, c.DateTo)
			},
		},
		{
			name:          "invalid filter returns error",
			payload:       &querysvc.QueryResourcesCountPayload{Filters: []string{"bad"}},
			expectError:   true,
			errorContains: "invalid filter",
		},
		{
			name:          "date_from without date_field returns error",
			payload:       &querysvc.QueryResourcesCountPayload{DateFrom: stringPtr("2025-01-01")},
			expectError:   true,
			errorContains: "date_field is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.payloadToCountPublicCriteria(tc.payload)
			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}
			assert.NoError(t, err)
			if tc.check != nil {
				tc.check(t, result)
			}
		})
	}
}

func TestPayloadToCountPrivateCriteria(t *testing.T) {
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	tests := []struct {
		name          string
		payload       *querysvc.QueryResourcesCountPayload
		expectError   bool
		errorContains string
		check         func(*testing.T, model.SearchCriteria)
	}{
		{
			name:    "empty payload sets private-only defaults",
			payload: &querysvc.QueryResourcesCountPayload{},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.True(t, c.PrivateOnly)
				assert.False(t, c.PublicOnly)
				assert.Equal(t, 0, c.PageSize)
			},
		},
		{
			name: "sets name, parent, type from payload",
			payload: &querysvc.QueryResourcesCountPayload{
				Name:   stringPtr("my-project"),
				Parent: stringPtr("p1"),
				Type:   stringPtr("project"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("my-project"), c.Name)
				assert.Equal(t, stringPtr("p1"), c.Parent)
				assert.Equal(t, stringPtr("project"), c.ResourceType)
			},
		},
		{
			name: "date range is applied",
			payload: &querysvc.QueryResourcesCountPayload{
				DateField: stringPtr("updated_at"),
				DateFrom:  stringPtr("2025-06-01"),
			},
			check: func(t *testing.T, c model.SearchCriteria) {
				assert.Equal(t, stringPtr("data.updated_at"), c.DateField)
				assert.NotNil(t, c.DateFrom)
				assert.Nil(t, c.DateTo)
			},
		},
		{
			name:          "invalid filters_all returns error",
			payload:       &querysvc.QueryResourcesCountPayload{FiltersAll: []string{"no-colon"}},
			expectError:   true,
			errorContains: "invalid filter",
		},
		{
			name:          "date_to without date_field returns error",
			payload:       &querysvc.QueryResourcesCountPayload{DateTo: stringPtr("2025-12-31")},
			expectError:   true,
			errorContains: "date_field is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.payloadToCountPrivateCriteria(tc.payload)
			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}
			assert.NoError(t, err)
			if tc.check != nil {
				tc.check(t, result)
			}
		})
	}
}

func TestPayloadToCountAggregation(t *testing.T) {
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	intPtr := func(v int) *int { return &v }

	tests := []struct {
		name          string
		payload       *querysvc.QueryResourcesCountPayload
		expectError   bool
		errorContains string
		expected      model.CountAggregation
	}{
		{
			name:     "empty payload asks for nothing",
			payload:  &querysvc.QueryResourcesCountPayload{},
			expected: model.CountAggregation{GroupBySize: constants.DefaultGroupBySize},
		},
		{
			name:     "group_by sets the prefix with the default size",
			payload:  &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("project_uid")},
			expected: model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: constants.DefaultGroupBySize},
		},
		{
			name:     "group_by_size overrides the default",
			payload:  &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("meeting_type"), GroupBySize: intPtr(1)},
			expected: model.CountAggregation{GroupByPrefix: "meeting_type", GroupBySize: 1},
		},
		{
			name:          "group_by_size above the maximum is rejected",
			payload:       &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("x"), GroupBySize: intPtr(constants.MaxGroupBySize + 1)},
			expectError:   true,
			errorContains: "group_by_size must be between 1 and 1000",
		},
		{
			name:          "group_by_size below one is rejected",
			payload:       &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("x"), GroupBySize: intPtr(0)},
			expectError:   true,
			errorContains: "group_by_size must be between 1 and 1000",
		},
		{
			name:          "group_by outside the tag prefix pattern is rejected",
			payload:       &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("Project-UID")},
			expectError:   true,
			errorContains: "group_by must match",
		},
		{
			name:     "cardinality metric sets the prefix",
			payload:  &querysvc.QueryResourcesCountPayload{Metric: stringPtr("cardinality:email")},
			expected: model.CountAggregation{CardinalityPrefix: "email", GroupBySize: constants.DefaultGroupBySize},
		},
		{
			name:          "sum metric is declined with the flat_object reason",
			payload:       &querysvc.QueryResourcesCountPayload{Metric: stringPtr("sum:duration")},
			expectError:   true,
			errorContains: "sum is not available on this index",
		},
		{
			name:          "unknown metric family is declined",
			payload:       &querysvc.QueryResourcesCountPayload{Metric: stringPtr("avg:duration")},
			expectError:   true,
			errorContains: "metric must be cardinality:<tag_prefix>",
		},
		{
			name:          "cardinality with an invalid prefix is declined",
			payload:       &querysvc.QueryResourcesCountPayload{Metric: stringPtr("cardinality:E-Mail")},
			expectError:   true,
			errorContains: "metric must be cardinality:<tag_prefix>",
		},
		{
			name:          "cardinality with an empty prefix is declined",
			payload:       &querysvc.QueryResourcesCountPayload{Metric: stringPtr("cardinality:")},
			expectError:   true,
			errorContains: "metric must be cardinality:<tag_prefix>",
		},
		{
			name:          "group_by and metric together are rejected",
			payload:       &querysvc.QueryResourcesCountPayload{GroupBy: stringPtr("project_uid"), Metric: stringPtr("cardinality:email")},
			expectError:   true,
			errorContains: "metric per group is not supported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.payloadToCountAggregation(tc.payload)
			if tc.expectError {
				assert.Error(t, err)
				var validation errors.Validation
				assert.ErrorAs(t, err, &validation, "count parameter errors must map to 400")
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDomainCountResultToResponse(t *testing.T) {
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)

	boolPtr := func(v bool) *bool { return &v }
	uint64Ptr := func(v uint64) *uint64 { return &v }
	cacheControl := "public, max-age=300"

	t.Run("plain count omits groups and metric", func(t *testing.T) {
		response := svc.domainCountResultToResponse(&model.CountResult{Count: 5, HasMore: true, CacheControl: &cacheControl})
		assert.Equal(t, uint64(5), response.Count)
		assert.True(t, response.HasMore)
		assert.Nil(t, response.Groups)
		assert.Nil(t, response.GroupsComplete)
		assert.Nil(t, response.MetricValue)
		assert.Nil(t, response.MetricComplete)
		assert.Equal(t, &cacheControl, response.CacheControl)
	})

	t.Run("grouped count renders groups in domain order", func(t *testing.T) {
		response := svc.domainCountResultToResponse(&model.CountResult{
			Count:          5,
			Groups:         []model.CountGroup{{Key: "P1", Count: 2}, {Key: "P2", Count: 2}, {Key: "P3", Count: 1}},
			GroupsComplete: boolPtr(true),
		})
		assert.Len(t, response.Groups, 3)
		assert.Equal(t, "P1", response.Groups[0].Key)
		assert.Equal(t, uint64(2), response.Groups[0].Count)
		assert.Equal(t, "P3", response.Groups[2].Key)
		assert.Equal(t, boolPtr(true), response.GroupsComplete)
	})

	t.Run("grouped count with no groups: groups_complete is the presence signal on the wire", func(t *testing.T) {
		// Goa renders optional arrays with omitempty, so an empty groups slice
		// is dropped from the JSON body; groups_complete (a pointer) survives
		// and tells the client group_by was honoured. Pin that wire shape.
		response := svc.domainCountResultToResponse(&model.CountResult{GroupsComplete: boolPtr(true)})
		assert.Len(t, response.Groups, 0)
		assert.NotNil(t, response.GroupsComplete)

		body := server.NewQueryResourcesCountResponseBody(response)
		encoded, err := json.Marshal(body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"count":0,"has_more":false,"groups_complete":true}`, string(encoded))
	})

	t.Run("metric renders value and completeness", func(t *testing.T) {
		response := svc.domainCountResultToResponse(&model.CountResult{
			Count:          4,
			MetricValue:    uint64Ptr(2),
			MetricComplete: boolPtr(false),
		})
		assert.Equal(t, uint64Ptr(2), response.MetricValue)
		assert.Equal(t, boolPtr(false), response.MetricComplete)
		assert.Nil(t, response.Groups)
	})
}

// newTestQuerySvc wires the service with the default resource search config.
func newTestQuerySvc(t *testing.T, searcher port.ResourceSearcher, checker port.AccessControlChecker, orgs port.OrganizationSearcher, auth port.Authenticator) *querySvcsrvc {
	t.Helper()
	svc, err := NewQuerySvc(searcher, checker, mock.NewMockResourceFilter(), orgs, auth, service.DefaultConfig())
	if err != nil {
		t.Fatalf("NewQuerySvc: %v", err)
	}
	return svc.(*querySvcsrvc)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
