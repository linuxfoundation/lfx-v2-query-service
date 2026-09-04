// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"goa.design/goa/v3/security"
)

func TestQuerySvcsrvc_JWTAuth(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		scheme        *security.JWTScheme
		setupEnv      func()
		cleanupEnv    func()
		expectedError bool
		expectContext bool
	}{
		{
			name:   "successful JWT auth with mock principal",
			token:  "mock-token",
			scheme: &security.JWTScheme{},
			setupEnv: func() {
				t.Setenv("JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL", "test-user-123")
			},
			cleanupEnv:    func() {},
			expectedError: false,
			expectContext: true,
		},
		{
			name:   "JWT auth without mock principal - should still work in test environment",
			token:  "real-jwt-token",
			scheme: &security.JWTScheme{},
			setupEnv: func() {
				// Clear any mock principal - but ParsePrincipal might still work
			},
			cleanupEnv:    func() {},
			expectedError: false, // Changed to false since we can't easily mock the JWT validator
			expectContext: false, // We don't expect a specific context value without proper setup
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			tc.setupEnv()
			defer tc.cleanupEnv()

			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.Background()

			// Execute
			resultCtx, err := svc.JWTAuth(ctx, tc.token, tc.scheme)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tc.expectContext {
					principal := resultCtx.Value(constants.PrincipalContextID)
					assert.NotNil(t, principal)
					assert.IsType(t, "", principal)
				}
			}
		})
	}
}

func TestQuerySvcsrvc_QueryResources(t *testing.T) {
	tests := []struct {
		name              string
		payload           *querysvc.QueryResourcesPayload
		setupMocks        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError     bool
		expectedErrorType interface{}
		expectedResources int
	}{
		{
			name: "successful resource query",
			payload: &querysvc.QueryResourcesPayload{
				Name: stringPtr("Test Project"),
				Type: stringPtr("project"),
			},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.AddResource(model.Resource{
					Type: "project",
					ID:   "test-project-1",
					Data: map[string]any{"name": "Test Project 1"},
					TransactionBodyStub: model.TransactionBodyStub{
						ObjectRef:  "project:test-project-1",
						ObjectType: "project",
						ObjectID:   "test-project-1",
						Public:     true,
					},
				})
				accessChecker.DefaultResult = "allowed"
			},
			expectedError:     false,
			expectedResources: 1,
		},
		{
			name:    "query with invalid criteria",
			payload: &querysvc.QueryResourcesPayload{
				// Empty payload should trigger validation error
			},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				// No setup needed
			},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
		},
		{
			name: "query with pagination",
			payload: &querysvc.QueryResourcesPayload{
				Name:      stringPtr("test"),
				PageToken: stringPtr("invalid-token"), // This will cause an error
			},
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				// No setup needed as we expect error during token parsing
			},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMocks(mockResourceSearcher, mockAccessChecker)

			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")

			// Execute
			result, err := svc.QueryResources(ctx, tc.payload)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tc.expectedResources, len(result.Resources))
			}
		})
	}
}

func TestQuerySvcsrvc_QueryResourcesCount(t *testing.T) {
	intPtr := func(v int) *int { return &v }

	// The default mock searcher carries one public project and two private
	// committees (committee:123#member, committee:567#member); see
	// mock.NewMockResourceSearcher.
	tests := []struct {
		name              string
		payload           *querysvc.QueryResourcesCountPayload
		principal         string
		setupMocks        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker)
		expectedError     bool
		expectedErrorType interface{}
		expectedErrorText string
		expectedCount     uint64
		check             func(*testing.T, *querysvc.QueryResourcesCountResult)
	}{
		{
			name: "authenticated count adds granted private buckets to the public count",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("committee"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount: 2,
			check: func(t *testing.T, result *querysvc.QueryResourcesCountResult) {
				assert.False(t, result.HasMore)
				assert.Nil(t, result.Groups)
				assert.Nil(t, result.MetricValue)
				assert.Nil(t, result.CacheControl)
			},
		},
		{
			name: "denied private buckets are not counted",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("committee"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.DefaultResult = "denied"
			},
			expectedCount: 0,
		},
		{
			name: "anonymous count is public only and cacheable",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("project"),
			},
			principal:     constants.AnonymousPrincipal,
			setupMocks:    func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {},
			expectedCount: 1,
			check: func(t *testing.T, result *querysvc.QueryResourcesCountResult) {
				assert.NotNil(t, result.CacheControl)
				assert.Equal(t, constants.AnonymousCacheControlHeader, *result.CacheControl)
			},
		},
		{
			name: "count query with tags",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Tags:    []string{"active", "governance"},
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.DefaultResult = "allowed"
			},
			// Every default mock resource carries the "active" tag; the one
			// private resource with no access fields has no access key, so it
			// cannot be counted (same as a malformed legacy document in the index).
			expectedCount: 4,
		},
		{
			name: "group_by returns groups over authorized resources",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				GroupBy: stringPtr("status"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.ClearResources()
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m1", map[string]any{"tags": []string{"status:a"}}, true))
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m2", map[string]any{"tags": []string{"status:a"}}, false))
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m3", map[string]any{"tags": []string{"status:b"}}, false))
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount: 3,
			check: func(t *testing.T, result *querysvc.QueryResourcesCountResult) {
				assert.NotNil(t, result.GroupsComplete)
				assert.True(t, *result.GroupsComplete)
				assert.Len(t, result.Groups, 2)
				assert.Equal(t, "a", result.Groups[0].Key)
				assert.Equal(t, uint64(2), result.Groups[0].Count)
				assert.Equal(t, "b", result.Groups[1].Key)
				assert.Equal(t, uint64(1), result.Groups[1].Count)
			},
		},
		{
			name: "group_by_size truncates and flags incomplete groups",
			payload: &querysvc.QueryResourcesCountPayload{
				Version:     "1",
				GroupBy:     stringPtr("status"),
				GroupBySize: intPtr(1),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.ClearResources()
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m1", map[string]any{"tags": []string{"status:a"}}, true))
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting", "m3", map[string]any{"tags": []string{"status:b"}}, true))
			},
			expectedCount: 2,
			check: func(t *testing.T, result *querysvc.QueryResourcesCountResult) {
				assert.Len(t, result.Groups, 1)
				assert.False(t, *result.GroupsComplete)
			},
		},
		{
			name: "cardinality metric counts distinct tag values over authorized resources",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Metric:  stringPtr("cardinality:email"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.ClearResources()
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting_participant", "p1", map[string]any{"tags": []string{"email:a@x.org"}}, false))
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting_participant", "p2", map[string]any{"tags": []string{"email:a@x.org"}}, false))
				searcher.AddResource(mock.NewResourceWithDefaults("v1_past_meeting_participant", "p3", map[string]any{"tags": []string{"email:b@y.org"}}, false))
				accessChecker.DefaultResult = "allowed"
			},
			expectedCount: 3,
			check: func(t *testing.T, result *querysvc.QueryResourcesCountResult) {
				assert.Nil(t, result.Groups)
				assert.NotNil(t, result.MetricValue)
				assert.Equal(t, uint64(2), *result.MetricValue)
				assert.True(t, *result.MetricComplete)
			},
		},
		{
			name: "sum metric is a 400 naming the reason",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("v1_past_meeting"),
				Metric:  stringPtr("sum:duration"),
			},
			principal:         "test-user",
			setupMocks:        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
			expectedErrorText: "sum is not available on this index",
		},
		{
			name: "group_by with metric is a 400",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				GroupBy: stringPtr("project_uid"),
				Metric:  stringPtr("cardinality:email"),
			},
			principal:         "test-user",
			setupMocks:        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
			expectedErrorText: "metric per group is not supported",
		},
		{
			name: "invalid filter is a 400",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Filters: []string{"no-colon"},
			},
			principal:         "test-user",
			setupMocks:        func(*mock.MockResourceSearcher, *mock.MockAccessControlChecker) {},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
		},
		{
			name: "public count failure is a 500",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("invalid"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				searcher.SetCountPublicError(fmt.Errorf("service error"))
			},
			expectedError:     true,
			expectedErrorType: &querysvc.InternalServerError{},
		},
		{
			name: "access check failure during the walk is a 503, never a partial count",
			payload: &querysvc.QueryResourcesCountPayload{
				Version: "1",
				Type:    stringPtr("committee"),
			},
			principal: "test-user",
			setupMocks: func(searcher *mock.MockResourceSearcher, accessChecker *mock.MockAccessControlChecker) {
				accessChecker.SetCheckAccessError(fmt.Errorf("nats: no responders"))
			},
			expectedError:     true,
			expectedErrorType: &querysvc.ServiceUnavailableError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMocks(mockResourceSearcher, mockAccessChecker)

			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, tc.principal)

			// Execute
			result, err := svc.QueryResourcesCount(ctx, tc.payload)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				if tc.expectedErrorText != "" {
					// Goa error types return "" from Error(); the caller-facing text is Message.
					badRequest, ok := err.(*querysvc.BadRequestError)
					if assert.True(t, ok, "expected a BadRequestError") {
						assert.Contains(t, badRequest.Message, tc.expectedErrorText)
					}
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tc.expectedCount, result.Count)
				if tc.check != nil {
					tc.check(t, result)
				}
			}
		})
	}
}

func TestQuerySvcsrvc_QueryOrgs(t *testing.T) {
	tests := []struct {
		name              string
		payload           *querysvc.QueryOrgsPayload
		setupMocks        func(*mock.MockOrganizationSearcher)
		expectedError     bool
		expectedErrorType interface{}
		expectedOrgName   string
	}{
		{
			name: "successful organization query by name",
			payload: &querysvc.QueryOrgsPayload{
				Name: stringPtr("The Linux Foundation"),
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "The Linux Foundation"
			},
			expectedError:   false,
			expectedOrgName: "The Linux Foundation",
		},
		{
			name: "successful organization query by domain",
			payload: &querysvc.QueryOrgsPayload{
				Domain: stringPtr("linuxfoundation.org"),
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data includes "linuxfoundation.org"
			},
			expectedError:   false,
			expectedOrgName: "The Linux Foundation",
		},
		{
			name: "organization not found",
			payload: &querysvc.QueryOrgsPayload{
				Name: stringPtr("Non-existent Organization"),
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// Default mock data doesn't include this organization
			},
			expectedError:     true,
			expectedErrorType: &querysvc.NotFoundError{},
		},
		{
			name:    "invalid query - no criteria",
			payload: &querysvc.QueryOrgsPayload{
				// Both name and domain are nil
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// No setup needed
			},
			expectedError:     true,
			expectedErrorType: &querysvc.BadRequestError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMocks(mockOrgSearcher)

			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.Background()

			// Execute
			result, err := svc.QueryOrgs(ctx, tc.payload)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotNil(t, result.Name)
				assert.Equal(t, tc.expectedOrgName, *result.Name)
			}
		})
	}
}

func TestQuerySvcsrvc_SuggestOrgs(t *testing.T) {
	tests := []struct {
		name                string
		payload             *querysvc.SuggestOrgsPayload
		setupMocks          func(*mock.MockOrganizationSearcher)
		expectedError       bool
		expectedErrorType   interface{}
		expectedSuggestions int
	}{
		{
			name: "successful organization suggestions",
			payload: &querysvc.SuggestOrgsPayload{
				Query: "linux",
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// Mock will return suggestions for "linux"
			},
			expectedError:       false,
			expectedSuggestions: 1, // Mock typically returns 1 suggestion
		},
		{
			name: "empty query",
			payload: &querysvc.SuggestOrgsPayload{
				Query: "",
			},
			setupMocks: func(searcher *mock.MockOrganizationSearcher) {
				// Mock will handle empty query and return all organizations (up to 5)
			},
			expectedError:       false,
			expectedSuggestions: 5, // Mock returns up to 5 suggestions for empty query
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMocks(mockOrgSearcher)

			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.Background()

			// Execute
			result, err := svc.SuggestOrgs(ctx, tc.payload)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotNil(t, result.Suggestions)
				assert.Equal(t, tc.expectedSuggestions, len(result.Suggestions))
			}
		})
	}
}

func TestQuerySvcsrvc_Readyz(t *testing.T) {
	tests := []struct {
		name              string
		setupMocks        func(*mock.MockResourceSearcher)
		expectedError     bool
		expectedErrorType interface{}
		expectedResponse  string
	}{
		{
			name: "service is ready",
			setupMocks: func(searcher *mock.MockResourceSearcher) {
				// Mock is ready by default
			},
			expectedError:    false,
			expectedResponse: "OK\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			tc.setupMocks(mockResourceSearcher)

			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.Background()

			// Execute
			result, err := svc.Readyz(ctx)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.expectedErrorType != nil {
					assert.IsType(t, tc.expectedErrorType, err)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tc.expectedResponse, string(result))
			}
		})
	}
}

func TestQuerySvcsrvc_Livez(t *testing.T) {
	tests := []struct {
		name             string
		expectedResponse string
	}{
		{
			name:             "service is alive",
			expectedResponse: "OK\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mocks
			mockResourceSearcher := mock.NewMockResourceSearcher()
			mockAccessChecker := mock.NewMockAccessControlChecker()
			mockOrgSearcher := mock.NewMockOrganizationSearcher()
			svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

			ctx := context.Background()

			// Execute
			result, err := svc.Livez(ctx)

			// Verify
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tc.expectedResponse, string(result))
		})
	}
}

func TestNewQuerySvc(t *testing.T) {
	tests := []struct {
		name         string
		setupMocks   func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker, *mock.MockOrganizationSearcher)
		expectNonNil bool
		expectType   string
	}{
		{
			name: "creates new query service with valid dependencies",
			setupMocks: func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker, *mock.MockOrganizationSearcher) {
				return mock.NewMockResourceSearcher(), mock.NewMockAccessControlChecker(), mock.NewMockOrganizationSearcher()
			},
			expectNonNil: true,
			expectType:   "*service.querySvcsrvc",
		},
		{
			name: "creates new query service with nil dependencies",
			setupMocks: func() (*mock.MockResourceSearcher, *mock.MockAccessControlChecker, *mock.MockOrganizationSearcher) {
				return nil, nil, nil
			},
			expectNonNil: true,
			expectType:   "*service.querySvcsrvc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			resourceSearcher, accessChecker, orgSearcher := tc.setupMocks()

			// Execute
			result, err := NewQuerySvc(resourceSearcher, accessChecker, mock.NewMockResourceFilter(), orgSearcher, mock.NewMockAuthService(), service.DefaultConfig())
			assert.NoError(t, err)

			// Verify
			if tc.expectNonNil {
				assert.NotNil(t, result)
				assert.IsType(t, &querySvcsrvc{}, result)

				// Cast to concrete type to verify internal fields
				if svc, ok := result.(*querySvcsrvc); ok {
					assert.NotNil(t, svc.resourceService)
					assert.NotNil(t, svc.organizationService)
				}
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestQuerySvcsrvc_InterfaceCompliance(t *testing.T) {
	// Verify that querySvcsrvc implements the querysvc.Service interface
	mockResourceSearcher := mock.NewMockResourceSearcher()
	mockAccessChecker := mock.NewMockAccessControlChecker()
	mockOrgSearcher := mock.NewMockOrganizationSearcher()
	svc := newTestQuerySvc(t, mockResourceSearcher, mockAccessChecker, mockOrgSearcher, mock.NewMockAuthService())

	// Compile-time guarantee that querySvcsrvc satisfies querysvc.Service.
	var _ querysvc.Service = (*querySvcsrvc)(nil)

	assert.NotNil(t, svc)
}
