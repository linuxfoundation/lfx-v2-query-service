// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestPayloadToCriteria(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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
				Name: stringPtr("test-project"),
				Type: stringPtr("project"),
				Tags: []string{"active", "governance"},
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
				Parent: stringPtr("parent-id"),
				Name:   stringPtr("child-resource"),
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
				Name: stringPtr("test"),
				Sort: "name_asc",
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
				Name: stringPtr("test"),
				Sort: "name_desc",
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
				Name: stringPtr("test"),
				Sort: "updated_asc",
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
				Name: stringPtr("test"),
				Sort: "updated_desc",
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
			name: "payload with invalid page token",
			payload: &querysvc.QueryResourcesPayload{
				Name:      stringPtr("test"),
				PageToken: stringPtr("invalid-token"),
			},
			expectedCriteria: model.SearchCriteria{}, // Will be empty due to error
			expectedError:    true,
		},
		{
			name:    "empty payload",
			payload: &querysvc.QueryResourcesPayload{},
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
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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

func TestPayloadToCriteriaWithDateFilters(t *testing.T) {
	// Setup service for testing
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	mockAuth := mock.NewMockAuthService()
	service := NewQuerySvc(mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mockAuth)
	svc := service.(*querySvcsrvc)

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

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
