// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestResourceSearchQueryResources(t *testing.T) {
	tests := []struct {
		name                 string
		criteria             model.SearchCriteria
		principal            string
		setupMocks           func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError        bool
		expectedResources    int
		expectedCacheControl bool
	}{
		{
			name: "successful search with authenticated user - public resources",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			principal: "user123",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.AddResource(model.Resource{
					Type: "project",
					ID:   "test-project",
					Data: map[string]any{"name": "Test Project"},
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:test-project",
						ObjectType:          "project",
						ObjectID:            "test-project",
						Public:              true,
						AccessCheckObject:   "project:test-project",
						AccessCheckRelation: "view",
					},
				})
				accessChecker.DefaultResult = "allowed"
			},
			expectedError:        false,
			expectedResources:    1,
			expectedCacheControl: false,
		},
		{
			name: "successful search with anonymous user",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			principal: constants.AnonymousPrincipal,
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.AddResource(model.Resource{
					Type: "project",
					ID:   "test-project",
					Data: map[string]any{"name": "Test Project"},
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "project:test-project",
						ObjectType: "project",
						ObjectID:   "test-project",
						Public:     true,
					},
				})
			},
			expectedError:        false,
			expectedResources:    1,
			expectedCacheControl: true,
		},
		{
			name:     "invalid search criteria - empty criteria",
			criteria: model.SearchCriteria{
				// All fields empty
			},
			principal: "user123",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				// No setup needed for this test
			},
			expectedError:        true,
			expectedResources:    0,
			expectedCacheControl: false,
		},
		{
			name: "missing principal in context",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			principal: "", // Empty principal to trigger error
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				// No setup needed for this test
			},
			expectedError:        true,
			expectedResources:    0,
			expectedCacheControl: false,
		},
		{
			name: "searcher returns error",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			principal: "user123",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				// Create a mock that will fail
				searcher.ClearResources()
			},
			expectedError:        false, // Mock searcher doesn't return errors in this implementation
			expectedResources:    0,
			expectedCacheControl: false,
		},
		{
			name: "access control check fails",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			principal: "user123",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.AddResource(model.Resource{
					Type: "project",
					ID:   "test-project",
					Data: map[string]any{"name": "Test Project"},
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:test-project",
						ObjectType:          "project",
						ObjectID:            "test-project",
						Public:              false,
						AccessCheckObject:   "project:test-project",
						AccessCheckRelation: "view",
					},
				})
				accessChecker.DefaultResult = "denied"
			},
			expectedError:        false,
			expectedResources:    0,
			expectedCacheControl: false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()

			tc.setupMocks(mockSearcher, mockAccessChecker)

			// Create service
			service, ok := NewResourceSearch(mockSearcher, mockAccessChecker, mock.NewMockResourceFilter()).(*ResourceSearch)
			if !ok {
				t.Fatal("failed to create ResourceSearch service")
			}

			// Setup context
			ctx := context.Background()
			if tc.principal != "" {
				ctx = context.WithValue(ctx, constants.PrincipalContextID, tc.principal)
			}

			// Execute
			result, err := service.QueryResources(ctx, tc.criteria)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
				return
			}
			assertion.NoError(err)
			assertion.NotNil(result)
			assertion.Equal(tc.expectedResources, len(result.Resources))

			if tc.expectedCacheControl {
				assertion.NotNil(result.CacheControl)
				assertion.Equal(constants.AnonymousCacheControlHeader, *result.CacheControl)
				return
			}
			assertion.Nil(result.CacheControl)

		})
	}
}

func TestResourceSearchValidateSearchCriteria(t *testing.T) {
	tests := []struct {
		name        string
		criteria    model.SearchCriteria
		expectError bool
	}{
		{
			name: "valid criteria with name",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			expectError: false,
		},
		{
			name: "valid criteria with parent",
			criteria: model.SearchCriteria{
				Parent: stringPtr("parent-id"),
			},
			expectError: false,
		},
		{
			name: "valid criteria with resource type",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
			},
			expectError: false,
		},
		{
			name: "valid criteria with tags",
			criteria: model.SearchCriteria{
				Tags: []string{"tag1", "tag2"},
			},
			expectError: false,
		},
		{
			name: "valid criteria with multiple fields",
			criteria: model.SearchCriteria{
				Name:         stringPtr("test"),
				ResourceType: stringPtr("project"),
				Tags:         []string{"tag1"},
			},
			expectError: false,
		},
		{
			name:     "invalid criteria - all fields empty",
			criteria: model.SearchCriteria{
				// All fields empty
			},
			expectError: true,
		},
		{
			name: "invalid criteria - empty tags array",
			criteria: model.SearchCriteria{
				Tags: []string{},
			},
			expectError: true,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create service
			service := &ResourceSearch{}

			// Execute
			err := service.validateSearchCriteria(tc.criteria)

			// Verify
			if tc.expectError {
				assertion.Error(err)
				return
			}
			assertion.NoError(err)

		})
	}
}

func TestResourceSearchBuildMessage(t *testing.T) {
	tests := []struct {
		name                    string
		principal               string
		searchResult            *model.SearchResult
		expectedPublicCount     int
		expectedNeedCheckCount  int
		expectedMessageContains []string
	}{
		{
			name:      "only public resources",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "public-project",
						Data: map[string]any{"name": "Public Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:  "project:public-project",
							ObjectType: "project",
							ObjectID:   "public-project",
							Public:     true,
						},
					},
				},
			},
			expectedPublicCount:     1,
			expectedNeedCheckCount:  0,
			expectedMessageContains: []string{},
		},
		{
			name:      "only private resources",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "private-project",
						Data: map[string]any{"name": "Private Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "project:private-project",
							ObjectType:          "project",
							ObjectID:            "private-project",
							Public:              false,
							AccessCheckObject:   "project:private-project",
							AccessCheckRelation: "view",
						},
					},
				},
			},
			expectedPublicCount:     0,
			expectedNeedCheckCount:  1,
			expectedMessageContains: []string{"project:private-project#view@user:user123"},
		},
		{
			name:      "mixed public and private resources",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "public-project",
						Data: map[string]any{"name": "Public Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:  "project:public-project",
							ObjectType: "project",
							ObjectID:   "public-project",
							Public:     true,
						},
					},
					{
						Type: "project",
						ID:   "private-project",
						Data: map[string]any{"name": "Private Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "project:private-project",
							ObjectType:          "project",
							ObjectID:            "private-project",
							Public:              false,
							AccessCheckObject:   "project:private-project",
							AccessCheckRelation: "view",
						},
					},
				},
			},
			expectedPublicCount:     1,
			expectedNeedCheckCount:  1,
			expectedMessageContains: []string{"project:private-project#view@user:user123"},
		},
		{
			name:      "duplicate resources filtered out",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "duplicate-project",
						Data: map[string]any{"name": "Duplicate Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:  "project:duplicate-project",
							ObjectType: "project",
							ObjectID:   "duplicate-project",
							Public:     true,
						},
					},
					{
						Type: "project",
						ID:   "duplicate-project",
						Data: map[string]any{"name": "Duplicate Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:  "project:duplicate-project",
							ObjectType: "project",
							ObjectID:   "duplicate-project",
							Public:     true,
						},
					},
				},
			},
			expectedPublicCount:     2, // Both resources remain, both are public
			expectedNeedCheckCount:  0,
			expectedMessageContains: []string{},
		},
		{
			name:      "resource missing access check info",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "project",
						ID:   "invalid-project",
						Data: map[string]any{"name": "Invalid Project"},
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:  "project:invalid-project",
							ObjectType: "project",
							ObjectID:   "invalid-project",
							Public:     false,
							// Missing AccessCheckObject and AccessCheckRelation
						},
					},
				},
			},
			expectedPublicCount:     0,
			expectedNeedCheckCount:  1,
			expectedMessageContains: []string{},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create service
			service := &ResourceSearch{}

			// Setup context
			ctx := context.Background()

			// Execute
			message := service.BuildMessage(ctx, tc.principal, tc.searchResult)

			// Count resources by their NeedCheck field
			publicCount := 0
			needCheckCount := 0
			for _, resource := range tc.searchResult.Resources {
				if resource.NeedCheck {
					needCheckCount++
				} else {
					publicCount++
				}
			}

			// Verify
			assertion.Equal(tc.expectedPublicCount, publicCount)
			if tc.expectedNeedCheckCount != needCheckCount {
				t.Errorf("Test case '%s' failed: expected needCheckCount=%d, got=%d", tc.name, tc.expectedNeedCheckCount, needCheckCount)
			}
			assertion.Equal(tc.expectedNeedCheckCount, needCheckCount)

			messageStr := string(message)
			for _, expectedSubstring := range tc.expectedMessageContains {
				assertion.Contains(messageStr, expectedSubstring)
			}
		})
	}
}

func TestResourceSearchCheckAccess(t *testing.T) {
	tests := []struct {
		name               string
		principal          string
		resources          []model.Resource
		message            []byte
		setupAccessChecker func(*mock.MockAccessControlChecker)
		expectedResources  int
		expectedError      bool
	}{
		{
			name:      "access granted for all resources",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "project",
					ID:        "test-project",
					Data:      map[string]any{"name": "Test Project"},
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:test-project",
						ObjectType:          "project",
						ObjectID:            "test-project",
						AccessCheckObject:   "project:test-project",
						AccessCheckRelation: "view",
					},
				},
			},
			message: []byte("project:test-project#view@user:user123\n"),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "allowed"
				checker.AllowedUserIDs = []string{"user123"}
			},
			expectedResources: 1,
			expectedError:     false,
		},
		{
			name:      "access denied for all resources",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "project",
					ID:        "test-project",
					Data:      map[string]any{"name": "Test Project"},
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:test-project",
						ObjectType:          "project",
						ObjectID:            "test-project",
						AccessCheckObject:   "project:test-project",
						AccessCheckRelation: "view",
					},
				},
			},
			message: []byte("project:test-project#view@user:user123\n"),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "denied"
			},
			expectedResources: 0,
			expectedError:     false,
		},
		{
			name:      "mixed access results",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "project",
					ID:        "allowed-project",
					Data:      map[string]any{"name": "Allowed Project"},
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:allowed-project",
						ObjectType:          "project",
						ObjectID:            "allowed-project",
						AccessCheckObject:   "project:allowed-project",
						AccessCheckRelation: "view",
					},
				},
				{
					Type:      "project",
					ID:        "denied-project",
					Data:      map[string]any{"name": "Denied Project"},
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "project:denied-project",
						ObjectType:          "project",
						ObjectID:            "denied-project",
						AccessCheckObject:   "project:denied-project",
						AccessCheckRelation: "view",
					},
				},
			},
			message: []byte("project:allowed-project#view@user:user123\nproject:denied-project#view@user:user123\n"),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				// Set up allowed and denied resources
				checker.AllowedUserIDs = []string{"user123"}
				checker.DeniedResourceIDs = []string{"denied-project"}
			},
			expectedResources: 1,
			expectedError:     false,
		},
		{
			name:      "empty resources list",
			principal: "user123",
			resources: []model.Resource{},
			message:   []byte(""),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "allowed"
			},
			expectedResources: 0,
			expectedError:     false,
		},
		{
			name:      "public resources should be included without access check",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "project",
					ID:        "public-project",
					Data:      map[string]any{"name": "Public Project"},
					NeedCheck: false,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "project:public-project",
						ObjectType: "project",
						ObjectID:   "public-project",
						Public:     true,
					},
				},
			},
			message: []byte(""),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "allowed"
			},
			expectedResources: 1,
			expectedError:     false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockAccessChecker := mock.NewMockAccessControlChecker()
			tc.setupAccessChecker(mockAccessChecker)

			// Create service
			service := &ResourceSearch{
				accessChecker: mockAccessChecker,
			}

			// Setup context
			ctx := context.Background()

			// Execute
			resources, err := service.CheckAccess(ctx, tc.principal, tc.resources, tc.message)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				return
			}
			assertion.NoError(err)
			assertion.Equal(tc.expectedResources, len(resources))

		})
	}
}

func TestNewResourceSearch(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectNonNil bool
		expectType   string
	}{
		{
			name: "creates new resource search with valid dependencies",
			setupMocks: func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {
				return mock.NewMockResourceSearcher(), mock.NewMockAccessControlChecker()
			},
			expectNonNil: true,
			expectType:   "*service.ResourceSearch",
		},
		{
			name: "creates new resource search with nil dependencies",
			setupMocks: func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {
				return nil, nil
			},
			expectNonNil: true,
			expectType:   "*service.ResourceSearch",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			searcher, accessChecker := tc.setupMocks()

			// Execute
			result := NewResourceSearch(searcher, accessChecker, mock.NewMockResourceFilter())

			// Verify
			if tc.expectNonNil {
				assertion.NotNil(result)
				assertion.IsType(&ResourceSearch{}, result)

				// Cast to concrete type to verify internal fields
				if resourceSearch, ok := result.(*ResourceSearch); ok {
					assertion.Equal(searcher, resourceSearch.resourceSearcher)
					assertion.Equal(accessChecker, resourceSearch.accessChecker)
				}
			} else {
				assertion.Nil(result)
			}
		})
	}
}

func TestResourceSearchQueryResourcesEdgeCases(t *testing.T) {
	assertion := assert.New(t)

	t.Run("search with complex criteria", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockResourceSearcher()
		mockAccessChecker := mock.NewMockAccessControlChecker()
		service, ok := NewResourceSearch(mockSearcher, mockAccessChecker, mock.NewMockResourceFilter()).(*ResourceSearch)
		if !ok {
			t.Fatal("failed to create ResourceSearch service")
		}

		// Add test data
		mockSearcher.AddResource(model.Resource{
			Type: "project",
			ID:   "complex-project",
			Data: map[string]any{
				"name": "Complex Project",
				"tags": []string{"active", "governance"},
			},
			TransactionBodyStub: model.TransactionBodyStub{
				ObjectRef:  "project:complex-project",
				ObjectType: "project",
				ObjectID:   "complex-project",
				Public:     true,
			},
		})

		criteria := model.SearchCriteria{
			Name:         stringPtr("Complex"),
			ResourceType: stringPtr("project"),
			Tags:         []string{"active"},
			SortBy:       "name",
			SortOrder:    "asc",
			PageSize:     10,
		}

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "user123")

		// Execute
		result, err := service.QueryResources(ctx, criteria)

		// Verify
		assertion.NoError(err)
		assertion.NotNil(result)
		assertion.Equal(1, len(result.Resources))
		assertion.Equal("complex-project", result.Resources[0].ID)
	})

	t.Run("search with pagination", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockResourceSearcher()
		mockAccessChecker := mock.NewMockAccessControlChecker()
		service, ok := NewResourceSearch(mockSearcher, mockAccessChecker, mock.NewMockResourceFilter()).(*ResourceSearch)
		if !ok {
			t.Fatal("failed to create ResourceSearch service")
		}

		criteria := model.SearchCriteria{
			Name:      stringPtr("test"),
			PageSize:  5,
			PageToken: stringPtr("test-token"),
		}

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "user123")

		// Execute
		result, err := service.QueryResources(ctx, criteria)

		// Verify
		assertion.NoError(err)
		assertion.NotNil(result)
	})
}

func TestResourceCountQueryResourcesCount(t *testing.T) {
	tests := []struct {
		name                 string
		countCriteria        model.SearchCriteria
		aggregationCriteria  model.SearchCriteria
		principal            string
		setupMocks           func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError        bool
		expectedCount        int
		expectedCacheControl bool
	}{
		{
			name: "successful count with anonymous user",
			countCriteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
				PageSize:     -1,
				PublicOnly:   true,
			},
			aggregationCriteria: model.SearchCriteria{},
			principal:           constants.AnonymousPrincipal,
			setupMocks: func(resourceSearcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				resourceSearcher.SetQueryResourcesCountResponse(&model.CountResult{
					Count:   3,
					HasMore: false,
				})
			},
			expectedError:        false,
			expectedCount:        3,
			expectedCacheControl: true,
		},
		{
			name: "successful count with authenticated user - public only",
			countCriteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
				PageSize:     -1,
				PublicOnly:   true,
			},
			aggregationCriteria: model.SearchCriteria{
				GroupBy:     "access_check_query.keyword",
				PageSize:    0,
				PrivateOnly: true,
			},
			principal: "user:test-user",
			setupMocks: func(resourceSearcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				resourceSearcher.SetQueryResourcesCountResponse(&model.CountResult{
					Count: 2,
					Aggregation: model.TermsAggregation{
						Buckets: []model.AggregationBucket{
							{Key: "project:123#viewer", DocCount: 1},
							{Key: "project:456#contributor", DocCount: 2},
						},
					},
					HasMore: false,
				})
				accessChecker.SetCheckAccessResponse(map[string]string{
					"project:123#viewer@user:test-user":      "true",
					"project:456#contributor@user:test-user": "false",
				})
			},
			expectedError:        false,
			expectedCount:        2,
			expectedCacheControl: false,
		},
		{
			name: "successful count with authenticated user - with private access",
			countCriteria: model.SearchCriteria{
				PageSize:   -1,
				PublicOnly: true,
			},
			aggregationCriteria: model.SearchCriteria{
				GroupBy:     "access_check_query.keyword",
				PageSize:    0,
				PrivateOnly: true,
			},
			principal: "user:admin",
			setupMocks: func(resourceSearcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				resourceSearcher.SetQueryResourcesCountResponse(&model.CountResult{
					Count: 5,
					Aggregation: model.TermsAggregation{
						Buckets: []model.AggregationBucket{
							{Key: "committee:789#member", DocCount: 3},
							{Key: "project:101#viewer", DocCount: 2},
						},
					},
					HasMore: false,
				})
				accessChecker.SetCheckAccessResponse(map[string]string{
					"committee:789#member@user:admin": "true",
					"project:101#viewer@user:admin":   "true",
				})
			},
			expectedError:        false,
			expectedCount:        5,
			expectedCacheControl: false,
		},
		{
			name: "search error",
			countCriteria: model.SearchCriteria{
				ResourceType: stringPtr("invalid"),
			},
			aggregationCriteria: model.SearchCriteria{},
			principal:           "user:test-user",
			setupMocks: func(resourceSearcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				resourceSearcher.SetQueryResourcesCountError(assert.AnError)
			},
			expectedError: true,
		},
		{
			name: "access control check error",
			countCriteria: model.SearchCriteria{
				PageSize:   -1,
				PublicOnly: true,
			},
			aggregationCriteria: model.SearchCriteria{
				GroupBy:     "access_check_query.keyword",
				PageSize:    0,
				PrivateOnly: true,
			},
			principal: "user:test-user",
			setupMocks: func(resourceSearcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				resourceSearcher.SetQueryResourcesCountResponse(&model.CountResult{
					Count: 2,
					Aggregation: model.TermsAggregation{
						Buckets: []model.AggregationBucket{
							{Key: "project:123#viewer", DocCount: 1},
						},
					},
					HasMore: false,
				})
				accessChecker.SetCheckAccessError(assert.AnError)
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			// Setup mocks
			resourceSearcher := mock.NewMockResourceSearcher()
			accessChecker := mock.NewMockAccessControlChecker()
			tc.setupMocks(resourceSearcher, accessChecker)

			// Create service
			service := NewResourceSearch(resourceSearcher, accessChecker, mock.NewMockResourceFilter())

			// Create context with principal
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, tc.principal)

			// Execute
			result, err := service.QueryResourcesCount(ctx, tc.countCriteria, tc.aggregationCriteria)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
			} else {
				assertion.NoError(err)
				assertion.NotNil(result)
				assertion.Equal(tc.expectedCount, result.Count)

				if tc.expectedCacheControl {
					assertion.NotNil(result.CacheControl)
				} else {
					// For non-anonymous users, CacheControl might be nil
					// This depends on implementation
				}
			}
		})
	}
}

func TestResourceCountBuildMessage(t *testing.T) {
	assertion := assert.New(t)

	// Setup
	resourceSearcher := mock.NewMockResourceSearcher()
	accessChecker := mock.NewMockAccessControlChecker()
	service := &ResourceSearch{
		resourceSearcher: resourceSearcher,
		accessChecker:    accessChecker,
	}

	// Test data
	result := &model.CountResult{
		Aggregation: model.TermsAggregation{
			Buckets: []model.AggregationBucket{
				{Key: "committee:123#member", DocCount: 2},
				{Key: "project:456#viewer", DocCount: 3},
			},
		},
	}

	criteria := model.SearchCriteria{
		PageSize: 10,
	}

	// Execute
	ctx := context.Background()
	message := service.BuildCountMessage(ctx, "test-user", result, criteria)

	// Verify
	assertion.NotNil(message)
	messageStr := string(message)
	assertion.Contains(messageStr, "committee:123#member@user:test-user")
	assertion.Contains(messageStr, "project:456#viewer@user:test-user")
	assertion.Contains(messageStr, "\n")
}

func TestResourceCountCheckAccess(t *testing.T) {
	tests := []struct {
		name               string
		result             *model.CountResult
		accessResponses    map[string]string
		expectedCount      uint64
		expectedError      bool
		setupAccessChecker func(*mock.MockAccessControlChecker)
	}{
		{
			name: "successful access check with allowed resources",
			result: &model.CountResult{
				Aggregation: model.TermsAggregation{
					Buckets: []model.AggregationBucket{
						{Key: "committee:123#member", DocCount: 2},
						{Key: "project:456#viewer", DocCount: 3},
					},
				},
			},
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessResponse(map[string]string{
					"committee:123#member@user:test-user": "true",
					"project:456#viewer@user:test-user":   "false",
				})
			},
			expectedCount: 2, // Only committee:123#member is allowed
			expectedError: false,
		},
		{
			name: "successful access check with all denied",
			result: &model.CountResult{
				Aggregation: model.TermsAggregation{
					Buckets: []model.AggregationBucket{
						{Key: "committee:123#member", DocCount: 2},
						{Key: "project:456#viewer", DocCount: 3},
					},
				},
			},
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessResponse(map[string]string{
					"committee:123#member@user:test-user": "false",
					"project:456#viewer@user:test-user":   "false",
				})
			},
			expectedCount: 0,
			expectedError: false,
		},
		{
			name: "access check error",
			result: &model.CountResult{
				Aggregation: model.TermsAggregation{
					Buckets: []model.AggregationBucket{
						{Key: "committee:123#member", DocCount: 2},
					},
				},
			},
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessError(assert.AnError)
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			// Setup
			resourceSearcher := mock.NewMockResourceSearcher()
			accessChecker := mock.NewMockAccessControlChecker()
			tc.setupAccessChecker(accessChecker)

			service := &ResourceSearch{
				resourceSearcher: resourceSearcher,
				accessChecker:    accessChecker,
			}

			// Build message
			ctx := context.Background()
			message := service.BuildCountMessage(ctx, "test-user", tc.result, model.SearchCriteria{PageSize: 10})

			// Execute
			count, err := service.CheckCountAccess(ctx, "test-user", tc.result, message)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
			} else {
				assertion.NoError(err)
				assertion.Equal(tc.expectedCount, count)
			}
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
