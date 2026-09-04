// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
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
			service := newTestResourceSearch(t, mockSearcher, mockAccessChecker)

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
		{
			name: "valid criteria with filter_grants and type",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
			},
			expectError: false,
		},
		{
			name: "valid criteria with filter_grants, type, and name",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
				Name:         stringPtr("standup"),
			},
			expectError: false,
		},
		{
			name: "invalid criteria - filter_grants without type",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
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
		expectedLineCount       int // 0 means "not checked"
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
			// Regression test: child resources (e.g. meeting registrants) are
			// deliberately assigned their parent's AccessCheckObject/Relation so
			// that many rows collapse to one check. BuildMessage must dedupe the
			// emitted check line by that key, not by ObjectRef, while still
			// marking every one of them NeedCheck=true (CheckAccess resolves
			// each resource independently via the shared response line).
			name:      "resources sharing one access check object emit a single line",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "meeting_registrant",
						ID:   "registrant-1",
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "meeting_registrant:registrant-1",
							ObjectType:          "meeting_registrant",
							ObjectID:            "registrant-1",
							Public:              false,
							AccessCheckObject:   "meeting:shared-meeting",
							AccessCheckRelation: "viewer",
						},
					},
					{
						Type: "meeting_registrant",
						ID:   "registrant-2",
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "meeting_registrant:registrant-2",
							ObjectType:          "meeting_registrant",
							ObjectID:            "registrant-2",
							Public:              false,
							AccessCheckObject:   "meeting:shared-meeting",
							AccessCheckRelation: "viewer",
						},
					},
					{
						Type: "meeting_registrant",
						ID:   "registrant-3",
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "meeting_registrant:registrant-3",
							ObjectType:          "meeting_registrant",
							ObjectID:            "registrant-3",
							Public:              false,
							AccessCheckObject:   "meeting:shared-meeting",
							AccessCheckRelation: "viewer",
						},
					},
				},
			},
			expectedPublicCount:    0,
			expectedNeedCheckCount: 3,
			expectedMessageContains: []string{
				"meeting:shared-meeting#viewer@user:user123",
			},
			expectedLineCount: 1,
		},
		{
			// Regression test: a Public resource `continue`s before touching
			// seenKeys (BuildMessage), so it must not suppress the check line
			// for a private resource sharing its AccessCheckObject/Relation,
			// nor should the private resource's own classification be affected
			// by the public one preceding it.
			name:      "public and private resource sharing one access check key",
			principal: "user123",
			searchResult: &model.SearchResult{
				Resources: []model.Resource{
					{
						Type: "meeting",
						ID:   "shared-meeting",
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "meeting:shared-meeting",
							ObjectType:          "meeting",
							ObjectID:            "shared-meeting",
							Public:              true,
							AccessCheckObject:   "meeting:shared-meeting",
							AccessCheckRelation: "viewer",
						},
					},
					{
						Type: "meeting_registrant",
						ID:   "registrant-1",
						TransactionBodyStub: model.TransactionBodyStub{
							ObjectRef:           "meeting_registrant:registrant-1",
							ObjectType:          "meeting_registrant",
							ObjectID:            "registrant-1",
							Public:              false,
							AccessCheckObject:   "meeting:shared-meeting",
							AccessCheckRelation: "viewer",
						},
					},
				},
			},
			expectedPublicCount:    1,
			expectedNeedCheckCount: 1,
			expectedMessageContains: []string{
				"meeting:shared-meeting#viewer@user:user123",
			},
			expectedLineCount: 1,
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
			if tc.expectedLineCount > 0 {
				assertion.Equal(tc.expectedLineCount, strings.Count(messageStr, "\n"))
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
			// Regression test: BuildMessage now dedupes the emitted line for
			// resources sharing one AccessCheckObject/Relation, so a single
			// response line must resolve every one of those resources here.
			name:      "single deduped line resolves all sharing resources",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "meeting_registrant",
					ID:        "registrant-1",
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "meeting_registrant:registrant-1",
						ObjectType:          "meeting_registrant",
						ObjectID:            "registrant-1",
						AccessCheckObject:   "meeting:shared-meeting",
						AccessCheckRelation: "viewer",
					},
				},
				{
					Type:      "meeting_registrant",
					ID:        "registrant-2",
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "meeting_registrant:registrant-2",
						ObjectType:          "meeting_registrant",
						ObjectID:            "registrant-2",
						AccessCheckObject:   "meeting:shared-meeting",
						AccessCheckRelation: "viewer",
					},
				},
			},
			message: []byte("meeting:shared-meeting#viewer@user:user123\n"),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "allowed"
				checker.AllowedUserIDs = []string{"user123"}
			},
			expectedResources: 2,
			expectedError:     false,
		},
		{
			// Regression test: the DENIED direction of the deduped line. A single
			// "false" response line must exclude every resource sharing that
			// AccessCheckObject/Relation key, not just the one whose ObjectRef
			// happens to match.
			name:      "single deduped line denies all sharing resources",
			principal: "user123",
			resources: []model.Resource{
				{
					Type:      "meeting_registrant",
					ID:        "registrant-1",
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "meeting_registrant:registrant-1",
						ObjectType:          "meeting_registrant",
						ObjectID:            "registrant-1",
						AccessCheckObject:   "meeting:shared-meeting",
						AccessCheckRelation: "viewer",
					},
				},
				{
					Type:      "meeting_registrant",
					ID:        "registrant-2",
					NeedCheck: true,
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:           "meeting_registrant:registrant-2",
						ObjectType:          "meeting_registrant",
						ObjectID:            "registrant-2",
						AccessCheckObject:   "meeting:shared-meeting",
						AccessCheckRelation: "viewer",
					},
				},
			},
			message: []byte("meeting:shared-meeting#viewer@user:user123\n"),
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.DefaultResult = "denied"
			},
			expectedResources: 0,
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
	assertion := assert.New(t)

	t.Run("creates new resource search with valid dependencies and defaults", func(t *testing.T) {
		searcher := mock.NewMockResourceSearcher()
		accessChecker := mock.NewMockAccessControlChecker()

		result, err := NewResourceSearch(searcher, accessChecker, mock.NewMockResourceFilter(), Config{})
		assertion.NoError(err)
		assertion.IsType(&ResourceSearch{}, result)

		resourceSearch := result.(*ResourceSearch)
		assertion.Equal(searcher, resourceSearch.resourceSearcher)
		assertion.Equal(accessChecker, resourceSearch.accessChecker)
		assertion.Equal(DefaultConfig(), resourceSearch.config)
	})

	t.Run("creates new resource search with nil dependencies", func(t *testing.T) {
		result, err := NewResourceSearch(nil, nil, mock.NewMockResourceFilter(), DefaultConfig())
		assertion.NoError(err)
		assertion.NotNil(result)
	})

	t.Run("explicit config is kept", func(t *testing.T) {
		config := Config{AccessCheckTimeout: time.Second, ReadTuplesTimeout: 2 * time.Second, AccessBucketPage: 2, MaxAccessBuckets: 3}
		result, err := NewResourceSearch(nil, nil, mock.NewMockResourceFilter(), config)
		assertion.NoError(err)
		assertion.Equal(config, result.(*ResourceSearch).config)
	})

	invalid := []struct {
		name   string
		config Config
		text   string
	}{
		{"negative access check timeout", Config{AccessCheckTimeout: -time.Second}, "access check timeout"},
		{"negative read tuples timeout", Config{ReadTuplesTimeout: -time.Second}, "read tuples timeout"},
		{"page above the maximum", Config{AccessBucketPage: constants.MaxAccessBucketPage + 1}, "access bucket page"},
		{"negative page", Config{AccessBucketPage: -1}, "access bucket page"},
		{"max below page", Config{AccessBucketPage: 100, MaxAccessBuckets: 50}, "max access buckets"},
	}
	for _, tc := range invalid {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			result, err := NewResourceSearch(nil, nil, mock.NewMockResourceFilter(), tc.config)
			assertion.Error(err)
			assertion.Contains(err.Error(), tc.text)
			assertion.Nil(result)
		})
	}
}

// newTestResourceSearch wires the service with the default config.
func newTestResourceSearch(t *testing.T, searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) *ResourceSearch {
	t.Helper()
	return newTestResourceSearchWithConfig(t, searcher, accessChecker, DefaultConfig())
}

// newTestResourceSearchWithConfig wires the service with an explicit config.
func newTestResourceSearchWithConfig(t *testing.T, searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker, config Config) *ResourceSearch {
	t.Helper()
	result, err := NewResourceSearch(searcher, accessChecker, mock.NewMockResourceFilter(), config)
	if err != nil {
		t.Fatalf("NewResourceSearch: %v", err)
	}
	return result.(*ResourceSearch)
}

func TestResourceSearchQueryResourcesEdgeCases(t *testing.T) {
	assertion := assert.New(t)

	t.Run("search with complex criteria", func(t *testing.T) {
		// Setup
		mockSearcher := mock.NewMockResourceSearcher()
		mockAccessChecker := mock.NewMockAccessControlChecker()
		service := newTestResourceSearch(t, mockSearcher, mockAccessChecker)

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
		service := newTestResourceSearch(t, mockSearcher, mockAccessChecker)

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
	publicCriteria := model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting"), PageSize: -1, PublicOnly: true}
	privateCriteria := model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting"), PageSize: 0, PrivateOnly: true}

	// Five past meetings: m1 public, m2..m5 private, one bucket each.
	seed := func(searcher *mock.MockResourceSearcher) {
		searcher.ClearResources()
		searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m1", map[string]any{"tags": []string{"project_uid:P1", "meeting_type:recurring"}}, true))
		searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m2", map[string]any{"tags": []string{"project_uid:P1", "meeting_type:single"}}, false))
		searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m3", map[string]any{"tags": []string{"project_uid:P2", "meeting_type:recurring"}}, false))
		searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m4", map[string]any{"tags": []string{"project_uid:P2", "meeting_type:recurring"}}, false))
		searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m5", map[string]any{"tags": []string{"project_uid:P3", "meeting_type:single"}}, false))
	}

	tests := []struct {
		name                 string
		principal            string
		config               Config
		aggregation          model.CountAggregation
		setupMocks           func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError        bool
		expectedUnavailable  bool
		expectedCount        int
		expectedHasMore      bool
		expectedPages        int
		expectedCacheControl bool
		check                func(*testing.T, *model.CountResult)
	}{
		{
			name:      "anonymous user gets the public count only, cacheable, no walk",
			principal: constants.AnonymousPrincipal,
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
			},
			expectedCount:        1,
			expectedPages:        0,
			expectedCacheControl: true,
		},
		{
			name:      "authenticated user, allow-all, walk of two pages is exhaustive",
			principal: "dev_user",
			config:    Config{AccessBucketPage: 2, MaxAccessBuckets: 5000},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount:   5,
			expectedHasMore: false,
			// page 1: 2 buckets (full), page 2: 2 buckets (full), page 3: 0 buckets (short)
			expectedPages: 3,
		},
		{
			name:      "cap reached after a full page stops the walk with has_more",
			principal: "dev_user",
			config:    Config{AccessBucketPage: 2, MaxAccessBuckets: 3},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "allowed"
			},
			// page 1 walks 2 (< 3, continue), page 2 walks 4 (>= 3, stop): all 4 private counted
			expectedCount:   5,
			expectedHasMore: true,
			expectedPages:   2,
		},
		{
			name:      "denied buckets are not counted",
			principal: "dev_user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.SetCheckAccessResponse(map[string]string{
					"v1_past_meeting:m2#viewer@user:dev_user": "true",
					"v1_past_meeting:m3#viewer@user:dev_user": "false",
					"v1_past_meeting:m4#viewer@user:dev_user": "true",
				})
			},
			expectedCount: 3,
			expectedPages: 1,
		},
		{
			name:      "deny-all yields the public count",
			principal: "dev_user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "denied"
			},
			expectedCount: 1,
			expectedPages: 1,
		},
		{
			name:      "public count error propagates",
			principal: "dev_user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.SetCountPublicError(assert.AnError)
			},
			expectedError: true,
		},
		{
			name:      "access bucket error propagates",
			principal: "dev_user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				searcher.SetAccessBucketsError(assert.AnError)
			},
			expectedError: true,
		},
		{
			name:      "access check failure mid-walk is service unavailable, not a partial count",
			principal: "dev_user",
			config:    Config{AccessBucketPage: 2, MaxAccessBuckets: 5000},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.SetCheckAccessError(assert.AnError)
			},
			expectedError:       true,
			expectedUnavailable: true,
		},
		{
			name:        "group_by runs over public plus granted resources",
			principal:   "dev_user",
			aggregation: model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount: 5,
			expectedPages: 1,
			check: func(t *testing.T, result *model.CountResult) {
				assert.Equal(t, []model.CountGroup{{Key: "P1", Count: 2}, {Key: "P2", Count: 2}, {Key: "P3", Count: 1}}, result.Groups)
				assert.NotNil(t, result.GroupsComplete)
				assert.True(t, *result.GroupsComplete)
				assert.Nil(t, result.MetricValue)
			},
		},
		{
			name:        "group_by for an anonymous user covers public resources only",
			principal:   constants.AnonymousPrincipal,
			aggregation: model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
			},
			expectedCount:        1,
			expectedCacheControl: true,
			check: func(t *testing.T, result *model.CountResult) {
				assert.Equal(t, []model.CountGroup{{Key: "P1", Count: 1}}, result.Groups)
			},
		},
		{
			name:        "group_by with everything denied and no public documents skips the aggregation",
			principal:   "dev_user",
			aggregation: model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "denied"
				// A forced aggregation response proves the searcher was consulted
				// (the service does not short-circuit): public documents may exist.
				searcher.SetAuthorizedAggregationResponse(&model.CountAggregationResult{Groups: []model.CountGroup{{Key: "P1", Count: 1}}, GroupsComplete: true})
			},
			expectedCount: 1,
			check: func(t *testing.T, result *model.CountResult) {
				assert.Equal(t, []model.CountGroup{{Key: "P1", Count: 1}}, result.Groups)
			},
		},
		{
			name:        "cardinality metric is reported with completeness",
			principal:   "dev_user",
			aggregation: model.CountAggregation{CardinalityPrefix: "project_uid"},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount: 5,
			check: func(t *testing.T, result *model.CountResult) {
				assert.Nil(t, result.GroupsComplete)
				assert.NotNil(t, result.MetricValue)
				assert.Equal(t, uint64(3), *result.MetricValue)
				assert.True(t, *result.MetricComplete)
			},
		},
		{
			name:        "aggregation error propagates",
			principal:   "dev_user",
			aggregation: model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				seed(searcher)
				accessChecker.DefaultResult = "allowed"
				searcher.SetAuthorizedAggregationError(assert.AnError)
			},
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			resourceSearcher := mock.NewMockResourceSearcher()
			accessChecker := mock.NewMockAccessControlChecker()
			tc.setupMocks(resourceSearcher, accessChecker)

			config := tc.config
			if config == (Config{}) {
				config = DefaultConfig()
			}
			service := newTestResourceSearchWithConfig(t, resourceSearcher, accessChecker, config)

			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, tc.principal)
			result, err := service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, tc.aggregation)

			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
				var unavailable errors.ServiceUnavailable
				assertion.Equal(tc.expectedUnavailable, stderrors.As(err, &unavailable), "service unavailable classification")
				return
			}

			assertion.NoError(err)
			assertion.NotNil(result)
			assertion.Equal(tc.expectedCount, result.Count)
			assertion.Equal(tc.expectedHasMore, result.HasMore)
			if tc.expectedPages > 0 || tc.principal == constants.AnonymousPrincipal {
				assertion.Equal(tc.expectedPages, resourceSearcher.AccessBucketCalls(), "pages walked")
			}
			if tc.expectedCacheControl {
				assertion.NotNil(result.CacheControl)
				assertion.Equal(constants.AnonymousCacheControlHeader, *result.CacheControl)
			} else {
				assertion.Nil(result.CacheControl)
			}
			if tc.check != nil {
				tc.check(t, result)
			}
		})
	}
}

func TestAccessBucketWalkPagesAndCaps(t *testing.T) {
	// Drive the walk with forced pages so the stopping rule is tested on its
	// own: three full pages of 100 followed by a short page of 50 at the
	// default cap is exhaustive; a cap of 100 stops after one full page.
	buckets := func(from, n int) []model.AggregationBucket {
		out := make([]model.AggregationBucket, 0, n)
		for i := from; i < from+n; i++ {
			out = append(out, model.AggregationBucket{Key: fmt.Sprintf("v1_past_meeting:m%03d#viewer", i), DocCount: 1})
		}
		return out
	}
	after := func(page []model.AggregationBucket) *string {
		last := page[len(page)-1].Key
		return &last
	}
	p1, p2, p3, p4 := buckets(0, 100), buckets(100, 100), buckets(200, 50), buckets(250, 0)
	publicCriteria := model.SearchCriteria{PublicOnly: true, PageSize: -1}
	privateCriteria := model.SearchCriteria{PrivateOnly: true}

	t.Run("250 buckets, default cap: three pages, exact, no has_more", func(t *testing.T) {
		assertion := assert.New(t)
		searcher := mock.NewMockResourceSearcher()
		searcher.SetCountPublicResponse(0)
		searcher.SetAccessBucketPages(
			&model.AccessBucketPage{Buckets: p1, AfterKey: after(p1)},
			&model.AccessBucketPage{Buckets: p2, AfterKey: after(p2)},
			&model.AccessBucketPage{Buckets: p3, AfterKey: after(p3)}, // short page with an after_key: must still stop
			&model.AccessBucketPage{Buckets: p4},
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		service := newTestResourceSearch(t, searcher, checker)

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "dev_user")
		result, err := service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, model.CountAggregation{})
		assertion.NoError(err)
		assertion.Equal(250, result.Count)
		assertion.False(result.HasMore)
		assertion.Equal(3, searcher.AccessBucketCalls())
	})

	t.Run("cap of 100: one full page, stop, has_more, private part exactly 100", func(t *testing.T) {
		assertion := assert.New(t)
		searcher := mock.NewMockResourceSearcher()
		searcher.SetCountPublicResponse(7)
		searcher.SetAccessBucketPages(
			&model.AccessBucketPage{Buckets: p1, AfterKey: after(p1)},
			&model.AccessBucketPage{Buckets: p2, AfterKey: after(p2)},
		)
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		service := newTestResourceSearchWithConfig(t, searcher, checker, Config{AccessBucketPage: 100, MaxAccessBuckets: 100})

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "dev_user")
		result, err := service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, model.CountAggregation{})
		assertion.NoError(err)
		assertion.Equal(107, result.Count)
		assertion.True(result.HasMore)
		assertion.Equal(1, searcher.AccessBucketCalls())
	})

	t.Run("full page without a cursor cannot be continued: has_more, never silently complete", func(t *testing.T) {
		assertion := assert.New(t)
		searcher := mock.NewMockResourceSearcher()
		searcher.SetCountPublicResponse(0)
		searcher.SetAccessBucketPages(&model.AccessBucketPage{Buckets: p1})
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		service := newTestResourceSearch(t, searcher, checker)

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "dev_user")
		result, err := service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, model.CountAggregation{})
		assertion.NoError(err)
		assertion.Equal(100, result.Count)
		assertion.True(result.HasMore)
		assertion.Equal(1, searcher.AccessBucketCalls())
	})

	t.Run("a capped walk marks groups and metric incomplete even when the aggregation itself was complete", func(t *testing.T) {
		assertion := assert.New(t)
		searcher := mock.NewMockResourceSearcher()
		searcher.SetCountPublicResponse(0)
		searcher.SetAccessBucketPages(
			&model.AccessBucketPage{Buckets: p1, AfterKey: after(p1)},
			&model.AccessBucketPage{Buckets: p2, AfterKey: after(p2)},
		)
		searcher.SetAuthorizedAggregationResponse(&model.CountAggregationResult{
			Groups:         []model.CountGroup{{Key: "P1", Count: 100}},
			GroupsComplete: true,
			MetricValue:    5,
			MetricComplete: true,
		})
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		service := newTestResourceSearchWithConfig(t, searcher, checker, Config{AccessBucketPage: 100, MaxAccessBuckets: 100})

		ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "dev_user")
		result, err := service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 10})
		assertion.NoError(err)
		assertion.True(result.HasMore)
		assertion.Equal([]model.CountGroup{{Key: "P1", Count: 100}}, result.Groups)
		assertion.False(*result.GroupsComplete, "groups were computed over a truncated authorized set")

		result, err = service.QueryResourcesCount(ctx, publicCriteria, privateCriteria, model.CountAggregation{CardinalityPrefix: "email"})
		assertion.NoError(err)
		assertion.True(result.HasMore)
		assertion.Equal(uint64(5), *result.MetricValue)
		assertion.False(*result.MetricComplete, "metric was computed over a truncated authorized set")
	})
}

func TestResourceCountBuildMessage(t *testing.T) {
	assertion := assert.New(t)

	service := newTestResourceSearch(t, mock.NewMockResourceSearcher(), mock.NewMockAccessControlChecker())

	buckets := []model.AggregationBucket{
		{Key: "committee:123#member", DocCount: 2},
		{Key: "project:456#viewer", DocCount: 3},
	}

	ctx := context.Background()
	message := service.BuildCountMessage(ctx, "test-user", buckets)

	// The wire format fga-sync parses: one "<key>@user:<principal>" line each,
	// newline-terminated (CheckCountAccess trims the trailing newline).
	assertion.Equal("committee:123#member@user:test-user\nproject:456#viewer@user:test-user\n", string(message))

	assertion.Empty(service.BuildCountMessage(ctx, "test-user", nil))
}

func TestResourceCountCheckAccess(t *testing.T) {
	buckets := []model.AggregationBucket{
		{Key: "committee:123#member", DocCount: 2},
		{Key: "project:456#viewer", DocCount: 3},
	}

	tests := []struct {
		name               string
		buckets            []model.AggregationBucket
		setupAccessChecker func(*mock.MockAccessControlChecker)
		expectedCount      uint64
		expectedGranted    []string
		expectedError      bool
	}{
		{
			name:    "successful access check with allowed resources",
			buckets: buckets,
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessResponse(map[string]string{
					"committee:123#member@user:test-user": "true",
					"project:456#viewer@user:test-user":   "false",
				})
			},
			expectedCount:   2, // Only committee:123#member is allowed
			expectedGranted: []string{"committee:123#member"},
		},
		{
			name:    "successful access check with all denied",
			buckets: buckets,
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessResponse(map[string]string{
					"committee:123#member@user:test-user": "false",
					"project:456#viewer@user:test-user":   "false",
				})
			},
			expectedCount:   0,
			expectedGranted: []string{},
		},
		{
			name:    "missing responses count as denied",
			buckets: buckets,
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessResponse(map[string]string{})
			},
			expectedCount:   0,
			expectedGranted: []string{},
		},
		{
			name:    "access check error is service unavailable",
			buckets: buckets[:1],
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) {
				checker.SetCheckAccessError(assert.AnError)
			},
			expectedError: true,
		},
		{
			name:               "empty page issues no check",
			buckets:            nil,
			setupAccessChecker: func(checker *mock.MockAccessControlChecker) { checker.SetCheckAccessError(assert.AnError) },
			expectedCount:      0,
			expectedGranted:    []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			accessChecker := mock.NewMockAccessControlChecker()
			tc.setupAccessChecker(accessChecker)
			service := newTestResourceSearch(t, mock.NewMockResourceSearcher(), accessChecker)

			ctx := context.Background()
			message := service.BuildCountMessage(ctx, "test-user", tc.buckets)
			count, granted, err := service.CheckCountAccess(ctx, "test-user", tc.buckets, message)

			if tc.expectedError {
				assertion.Error(err)
				var unavailable errors.ServiceUnavailable
				assertion.True(stderrors.As(err, &unavailable))
				return
			}
			assertion.NoError(err)
			assertion.Equal(tc.expectedCount, count)
			assertion.Equal(tc.expectedGranted, granted)
		})
	}
}

func TestResourceSearchFilterGrants(t *testing.T) {
	tests := []struct {
		name              string
		criteria          model.SearchCriteria
		principal         string
		setupMocks        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError     bool
		expectedResources int
	}{
		{
			name: "filter_grants=direct returns resources matching grants",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
			},
			principal: "jme",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.MockTupleRefs = []string{
					"v1_past_meeting:meeting-1",
					"v1_past_meeting:meeting-2",
				}
				searcher.AddResource(model.Resource{
					Type: "v1_past_meeting",
					ID:   "meeting-1",
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "v1_past_meeting:meeting-1",
						ObjectType: "v1_past_meeting",
						ObjectID:   "meeting-1",
						Public:     true,
					},
				})
				searcher.AddResource(model.Resource{
					Type: "v1_past_meeting",
					ID:   "meeting-2",
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "v1_past_meeting:meeting-2",
						ObjectType: "v1_past_meeting",
						ObjectID:   "meeting-2",
						Public:     true,
					},
				})
				// This resource is the same type but NOT in the tuple refs — it must be excluded.
				searcher.AddResource(model.Resource{
					Type: "v1_past_meeting",
					ID:   "meeting-3",
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "v1_past_meeting:meeting-3",
						ObjectType: "v1_past_meeting",
						ObjectID:   "meeting-3",
						Public:     true,
					},
				})
			},
			expectedError:     false,
			expectedResources: 2,
		},
		{
			name: "filter_grants=direct with no grants returns empty result",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
			},
			principal: "jme",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.MockTupleRefs = []string{}
			},
			expectedError:     false,
			expectedResources: 0,
		},
		{
			name: "filter_grants=direct fails for anonymous user",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
			},
			principal: constants.AnonymousPrincipal,
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
			},
			expectedError:     true,
			expectedResources: 0,
		},
		{
			name: "filter_grants=direct returns error when ReadTuples fails",
			criteria: model.SearchCriteria{
				FilterGrants: stringPtr("direct"),
				ResourceType: stringPtr("v1_past_meeting"),
			},
			principal: "jme",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.SimulateTuplesError = true
			},
			expectedError:     true,
			expectedResources: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			mockSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			tc.setupMocks(mockSearcher, mockAccessChecker)

			service := newTestResourceSearch(t, mockSearcher, mockAccessChecker)

			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, tc.principal)
			result, err := service.QueryResources(ctx, tc.criteria)

			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
				return
			}
			assertion.NoError(err)
			assertion.NotNil(result)
			assertion.Equal(tc.expectedResources, len(result.Resources))
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
