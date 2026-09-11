// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestOrganizationSearchQueryOrganizations(t *testing.T) {
	tests := []struct {
		name                    string
		criteria                model.OrganizationSearchCriteria
		setupMock               func(*mock.MockOrganizationSearcher)
		expectedError           bool
		expectedErrorType       interface{}
		expectedOrganization    *model.Organization
		expectedOrganizationNil bool
	}{
		{
			name: "successful search by name",
			criteria: model.OrganizationSearchCriteria{
				Name: stringPtr("The Linux Foundation"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "The Linux Foundation"
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "The Linux Foundation", Domain: "linuxfoundation.org", Industry: "Non-Profit", Sector: "Technology", Employees: "100-499"},
			expectedOrganizationNil: false,
		},
		{
			name: "successful search by domain",
			criteria: model.OrganizationSearchCriteria{
				Domain: stringPtr("linuxfoundation.org"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "linuxfoundation.org"
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "The Linux Foundation", Domain: "linuxfoundation.org", Industry: "Non-Profit", Sector: "Technology", Employees: "100-499"},
			expectedOrganizationNil: false,
		},
		{
			name: "successful search with case insensitive name",
			criteria: model.OrganizationSearchCriteria{
				Name: stringPtr("the linux foundation"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "The Linux Foundation"
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "The Linux Foundation", Domain: "linuxfoundation.org", Industry: "Non-Profit", Sector: "Technology", Employees: "100-499"},
			expectedOrganizationNil: false,
		},
		{
			name: "successful search with case insensitive domain",
			criteria: model.OrganizationSearchCriteria{
				Domain: stringPtr("LINUXFOUNDATION.ORG"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "linuxfoundation.org"
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "The Linux Foundation", Domain: "linuxfoundation.org", Industry: "Non-Profit", Sector: "Technology", Employees: "100-499"},
			expectedOrganizationNil: false,
		},
		{
			name: "organization not found by name",
			criteria: model.OrganizationSearchCriteria{
				Name: stringPtr("Non-existent Organization"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data doesn't include this organization
			},
			expectedError:           true,
			expectedErrorType:       errors.NotFound{},
			expectedOrganization:    nil,
			expectedOrganizationNil: true,
		},
		{
			name: "organization not found by domain",
			criteria: model.OrganizationSearchCriteria{
				Domain: stringPtr("non-existent.com"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data doesn't include this domain
			},
			expectedError:           true,
			expectedErrorType:       errors.NotFound{},
			expectedOrganization:    nil,
			expectedOrganizationNil: true,
		},
		{
			name: "organization not found with both name and domain",
			criteria: model.OrganizationSearchCriteria{
				Name:   stringPtr("Non-existent Organization"),
				Domain: stringPtr("non-existent.com"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data doesn't include this organization
			},
			expectedError:           true,
			expectedErrorType:       errors.NotFound{},
			expectedOrganization:    nil,
			expectedOrganizationNil: true,
		},
		{
			name:     "validation error - no search criteria",
			criteria: model.OrganizationSearchCriteria{
				// Both name and domain are nil
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// No setup needed
			},
			expectedError:           true,
			expectedErrorType:       errors.Validation{},
			expectedOrganization:    nil,
			expectedOrganizationNil: true,
		},
		{
			name: "search with custom organization",
			criteria: model.OrganizationSearchCriteria{
				Name: stringPtr("Custom Test Org"),
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				searcher.AddOrganization(model.Organization{
					Name:      "Custom Test Org",
					Domain:    "customtest.org",
					Industry:  "Testing",
					Sector:    "Quality Assurance",
					Employees: "50-100",
				})
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "Custom Test Org", Domain: "customtest.org", Industry: "Testing", Sector: "Quality Assurance", Employees: "50-100"},
			expectedOrganizationNil: false,
		},
		{
			name: "search returns first match when multiple criteria provided",
			criteria: model.OrganizationSearchCriteria{
				Name:   stringPtr("The Linux Foundation"),
				Domain: stringPtr("example.com"), // This domain doesn't exist but name does
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "The Linux Foundation"
			},
			expectedError:           false,
			expectedOrganization:    &model.Organization{Name: "The Linux Foundation", Domain: "linuxfoundation.org", Industry: "Non-Profit", Sector: "Technology", Employees: "100-499"},
			expectedOrganizationNil: false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMock(mockSearcher)

			// Create service
			service := NewOrganizationSearch(mockSearcher)

			// Setup context
			ctx := context.Background()

			// Execute
			result, err := service.QueryOrganizations(ctx, tc.criteria)

			// Verify error expectations
			if tc.expectedError {
				assertion.Error(err)
				if tc.expectedErrorType != nil {
					assertion.IsType(tc.expectedErrorType, err)
				}
			} else {
				assertion.NoError(err)
			}

			// Verify result expectations
			if tc.expectedOrganizationNil {
				assertion.Nil(result)
			} else {
				assertion.NotNil(result)
				if tc.expectedOrganization != nil {
					assertion.Equal(tc.expectedOrganization.Name, result.Name)
					assertion.Equal(tc.expectedOrganization.Domain, result.Domain)
					assertion.Equal(tc.expectedOrganization.Industry, result.Industry)
					assertion.Equal(tc.expectedOrganization.Sector, result.Sector)
					assertion.Equal(tc.expectedOrganization.Employees, result.Employees)
				}
			}
		})
	}
}

func TestOrganizationSearchSuggestOrganizations(t *testing.T) {
	tests := []struct {
		name                     string
		criteria                 model.OrganizationSuggestionCriteria
		setupMock                func(*mock.MockOrganizationSearcher)
		expectedError            bool
		expectedErrorType        interface{}
		expectedSuggestionsCount int
		expectedSuggestions      []model.OrganizationSuggestion
	}{
		{
			name: "successful suggestions search with query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "linux",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "The Linux Foundation"
			},
			expectedError:            false,
			expectedSuggestionsCount: 1,
			expectedSuggestions: []model.OrganizationSuggestion{
				{
					Name:   "The Linux Foundation",
					Domain: "linuxfoundation.org",
					Logo:   nil,
				},
			},
		},
		{
			name: "successful suggestions search with domain query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "linuxfoundation.org",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "linuxfoundation.org"
			},
			expectedError:            false,
			expectedSuggestionsCount: 1,
			expectedSuggestions: []model.OrganizationSuggestion{
				{
					Name:   "The Linux Foundation",
					Domain: "linuxfoundation.org",
					Logo:   nil,
				},
			},
		},
		{
			name: "suggestions search with partial match",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "found",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Should match "The Linux Foundation"
			},
			expectedError:            false,
			expectedSuggestionsCount: 1,
			expectedSuggestions: []model.OrganizationSuggestion{
				{
					Name:   "The Linux Foundation",
					Domain: "linuxfoundation.org",
					Logo:   nil,
				},
			},
		},
		{
			name: "suggestions search with no matches",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "nonexistent",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// No matches expected
			},
			expectedError:            false,
			expectedSuggestionsCount: 0,
			expectedSuggestions:      []model.OrganizationSuggestion{},
		},
		{
			name: "suggestions search with empty query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Empty query should match all organizations
			},
			expectedError:            false,
			expectedSuggestionsCount: 5, // Mock limits to 5 suggestions
		},
		{
			name: "suggestions search with case insensitive query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "LINUX",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Should match despite case difference
			},
			expectedError:            false,
			expectedSuggestionsCount: 1,
			expectedSuggestions: []model.OrganizationSuggestion{
				{
					Name:   "The Linux Foundation",
					Domain: "linuxfoundation.org",
					Logo:   nil,
				},
			},
		},
		{
			name: "suggestions search with multiple matches",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "tech",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Add multiple organizations with "tech" in name
				// Note: Mock already has "Blorbtech Intergalactic Solutions" which matches "tech"
				searcher.AddOrganization(model.Organization{
					Name:      "TechCorp",
					Domain:    "techcorp.com",
					Industry:  "Technology",
					Sector:    "Software",
					Employees: "50-100",
				})
				searcher.AddOrganization(model.Organization{
					Name:      "FinTech Solutions",
					Domain:    "fintech.io",
					Industry:  "Financial Technology",
					Sector:    "Finance",
					Employees: "200-500",
				})
			},
			expectedError:            false,
			expectedSuggestionsCount: 3, // Updated to account for existing mock data
		},
		// Note: Mock error and nil result tests would require extending the mock
		// to support function overrides, which is not currently implemented.
		// These scenarios would be tested in integration tests or with a more
		// sophisticated mock that supports error injection.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMock(mockSearcher)

			// Create service
			service := NewOrganizationSearch(mockSearcher)

			// Execute
			ctx := context.Background()
			result, err := service.SuggestOrganizations(ctx, tc.criteria)

			// Verify error
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				return
			}

			assert.NoError(t, err)

			// Verify result
			if tc.expectedSuggestionsCount > 0 {
				assert.NotNil(t, result)
				assert.Len(t, result.Suggestions, tc.expectedSuggestionsCount)

				// If specific suggestions are expected, verify them
				if len(tc.expectedSuggestions) > 0 {
					for i, expectedSuggestion := range tc.expectedSuggestions {
						if i < len(result.Suggestions) {
							assert.Equal(t, expectedSuggestion.Name, result.Suggestions[i].Name)
							assert.Equal(t, expectedSuggestion.Domain, result.Suggestions[i].Domain)
							assert.Equal(t, expectedSuggestion.Logo, result.Suggestions[i].Logo)
						}
					}
				}
			} else {
				// Handle case where no suggestions are expected
				if result != nil {
					assert.Len(t, result.Suggestions, 0)
				}
			}
		})
	}
}

func TestOrganizationSearchSuggestOrganizationsEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		criteria      model.OrganizationSuggestionCriteria
		setupMock     func(*mock.MockOrganizationSearcher)
		expectedError bool
		description   string
	}{
		{
			name: "suggestions search with whitespace query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "   linux   ",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Should still match despite whitespace
			},
			expectedError: false,
			description:   "Should handle queries with leading/trailing whitespace",
		},
		{
			name: "suggestions search with special characters",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "linux-foundation",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Add organization with special characters
				searcher.AddOrganization(model.Organization{
					Name:   "Linux-Foundation-Test",
					Domain: "linux-foundation.test",
				})
			},
			expectedError: false,
			description:   "Should handle queries with special characters",
		},
		{
			name: "suggestions search with unicode characters",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "café",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Add organization with unicode characters
				searcher.AddOrganization(model.Organization{
					Name:   "Café Technologies",
					Domain: "cafe-tech.com",
				})
			},
			expectedError: false,
			description:   "Should handle unicode characters in queries",
		},
		{
			name: "suggestions search with very long query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "this is a very long query string that might cause issues with some search implementations but should be handled gracefully",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// No special setup needed
			},
			expectedError: false,
			description:   "Should handle very long query strings",
		},
		{
			name: "suggestions search with numeric query",
			criteria: model.OrganizationSuggestionCriteria{
				Query: "123",
			},
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Add organization with numbers
				searcher.AddOrganization(model.Organization{
					Name:   "Company 123",
					Domain: "company123.com",
				})
			},
			expectedError: false,
			description:   "Should handle numeric queries",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMock(mockSearcher)

			// Create service
			service := NewOrganizationSearch(mockSearcher)

			// Execute
			ctx := context.Background()
			result, err := service.SuggestOrganizations(ctx, tc.criteria)

			// Verify
			if tc.expectedError {
				assert.Error(t, err, tc.description)
			} else {
				assert.NoError(t, err, tc.description)
				// Result should not be nil for successful queries, even if no matches found
				assert.NotNil(t, result, tc.description)
				if result != nil {
					assert.NotNil(t, result.Suggestions, tc.description)
				}
			}
		})
	}
}

func TestOrganizationSearchIsReady(t *testing.T) {
	tests := []struct {
		name              string
		setupMock         func(*mock.MockOrganizationSearcher)
		expectedError     bool
		expectedErrorType interface{}
	}{
		{
			name: "service is ready",
			setupMock: func(searcher *mock.MockOrganizationSearcher) {
				// Mock is always ready by default
			},
			expectedError: false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMock(mockSearcher)

			// Create service
			service := NewOrganizationSearch(mockSearcher)

			// Setup context
			ctx := context.Background()

			// Execute
			err := service.IsReady(ctx)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				if tc.expectedErrorType != nil {
					assertion.IsType(tc.expectedErrorType, err)
				}
			} else {
				assertion.NoError(err)
			}
		})
	}
}

func TestNewOrganizationSearch(t *testing.T) {
	tests := []struct {
		name         string
		setupMock    func() *mock.MockOrganizationSearcher
		expectNonNil bool
		expectType   string
	}{
		{
			name: "creates new organization search with valid dependency",
			setupMock: func() *mock.MockOrganizationSearcher {
				return mock.NewMockOrganizationSearcher()
			},
			expectNonNil: true,
			expectType:   "*service.OrganizationSearch",
		},
		{
			name: "creates new organization search with nil dependency",
			setupMock: func() *mock.MockOrganizationSearcher {
				return nil
			},
			expectNonNil: true,
			expectType:   "*service.OrganizationSearch",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			searcher := tc.setupMock()

			// Execute
			result := NewOrganizationSearch(searcher)

			// Verify
			if tc.expectNonNil {
				assertion.NotNil(result)
				assertion.IsType(&OrganizationSearch{}, result)

				// Cast to concrete type to verify internal fields
				if orgSearch, ok := result.(*OrganizationSearch); ok {
					assertion.Equal(searcher, orgSearch.organizationSearcher)
				}
			} else {
				assertion.Nil(result)
			}
		})
	}
}

func TestOrganizationSearchQueryOrganizationsEdgeCases(t *testing.T) {
	assertion := assert.New(t)

	t.Run("search with empty string name", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr(""),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - empty string should not match any organization
		assertion.Error(err)
		assertion.IsType(errors.NotFound{}, err)
		assertion.Nil(result)
	})

	t.Run("search with empty string domain", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Domain: stringPtr(""),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - empty string should not match any organization
		assertion.Error(err)
		assertion.IsType(errors.NotFound{}, err)
		assertion.Nil(result)
	})

	t.Run("search with whitespace-only name", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr("   "),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - whitespace-only string should not match any organization
		assertion.Error(err)
		assertion.IsType(errors.NotFound{}, err)
		assertion.Nil(result)
	})

	t.Run("search with whitespace-only domain", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Domain: stringPtr("   "),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - whitespace-only string should not match any organization
		assertion.Error(err)
		assertion.IsType(errors.NotFound{}, err)
		assertion.Nil(result)
	})

	t.Run("search with cleared mock data", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		mockSearcher.ClearOrganizations() // Remove all organizations
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr("Any Organization"),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - no organizations should be found
		assertion.Error(err)
		assertion.IsType(errors.NotFound{}, err)
		assertion.Nil(result)
	})

	t.Run("search with context cancellation", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr("The Linux Foundation"),
		}

		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Note: The mock implementation doesn't check for context cancellation,
		// but in a real implementation this would return an error.
		// For now, we test that the service still works with a canceled context
		assertion.NoError(err)
		assertion.NotNil(result)
		assertion.Equal("The Linux Foundation", result.Name)
	})

	t.Run("search with multiple organizations having similar names", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		mockSearcher.ClearOrganizations()

		// Add organizations with similar names
		mockSearcher.AddOrganization(model.Organization{
			Name:   "Test Organization",
			Domain: "test1.org",
		})
		mockSearcher.AddOrganization(model.Organization{
			Name:   "Test Organization Inc",
			Domain: "test2.org",
		})

		service := NewOrganizationSearch(mockSearcher)

		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr("Test Organization"),
		}

		ctx := context.Background()

		// Execute
		result, err := service.QueryOrganizations(ctx, criteria)

		// Verify - should find exact match
		assertion.NoError(err)
		assertion.NotNil(result)
		assertion.Equal("Test Organization", result.Name)
		assertion.Equal("test1.org", result.Domain)
	})
}

func TestOrganizationSearchInterface(t *testing.T) {
	assertion := assert.New(t)

	t.Run("OrganizationSearch implements OrganizationSearcher interface", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		// Compile-time guarantee that OrganizationSearch satisfies OrganizationSearcher.
		var _ OrganizationSearcher = (*OrganizationSearch)(nil)
		assertion.NotNil(service)
	})

	t.Run("interface methods are callable", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockOrganizationSearcher()
		service := NewOrganizationSearch(mockSearcher)

		ctx := context.Background()
		criteria := model.OrganizationSearchCriteria{
			Name: stringPtr("The Linux Foundation"),
		}

		// Test QueryOrganizations method through interface
		result, err := service.QueryOrganizations(ctx, criteria)
		assertion.NoError(err)
		assertion.NotNil(result)

		// Test IsReady method through interface
		err = service.IsReady(ctx)
		assertion.NoError(err)
	})
}
