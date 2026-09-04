// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
)

// MockOpenSearchClient is a mock implementation of OpenSearchClientRetriever
type MockOpenSearchClient struct {
	searchResponse *SearchResponse
	searchError    error
	countResponse  *CountResponse
	countError     error
	// aggregationResponses are returned in order by successive
	// AggregationSearch calls; the last one repeats once exhausted.
	aggregationResponses []json.RawMessage
	aggregationError     error
	aggregationCalls     int
	aggregationQueries   [][]byte
	mappingResponse      *IndexMapping
	mappingError         error
	mappingCalls         int
	lastPageSize         int
}

func NewMockOpenSearchClient() *MockOpenSearchClient {
	return &MockOpenSearchClient{}
}

func (m *MockOpenSearchClient) Search(ctx context.Context, index string, query []byte, pageSize int) (*SearchResponse, error) {
	m.lastPageSize = pageSize
	if m.searchError != nil {
		return nil, m.searchError
	}
	return m.searchResponse, nil
}

func (m *MockOpenSearchClient) Count(ctx context.Context, index string, query []byte) (*CountResponse, error) {
	if m.countError != nil {
		return nil, m.countError
	}
	return m.countResponse, nil
}

func (m *MockOpenSearchClient) AggregationSearch(ctx context.Context, index string, query []byte) (json.RawMessage, error) {
	m.aggregationCalls++
	m.aggregationQueries = append(m.aggregationQueries, query)
	if m.aggregationError != nil {
		return nil, m.aggregationError
	}
	if len(m.aggregationResponses) == 0 {
		return nil, nil
	}
	idx := m.aggregationCalls - 1
	if idx >= len(m.aggregationResponses) {
		idx = len(m.aggregationResponses) - 1
	}
	return m.aggregationResponses[idx], nil
}

func (m *MockOpenSearchClient) GetMapping(ctx context.Context, index string) (*IndexMapping, error) {
	m.mappingCalls++
	if m.mappingError != nil {
		return nil, m.mappingError
	}
	return m.mappingResponse, nil
}

func (m *MockOpenSearchClient) SetSearchResponse(response *SearchResponse) {
	m.searchResponse = response
}

func (m *MockOpenSearchClient) SetSearchError(err error) {
	m.searchError = err
}

func (m *MockOpenSearchClient) SetCountResponse(response *CountResponse) {
	m.countResponse = response
}

func (m *MockOpenSearchClient) SetCountError(err error) {
	m.countError = err
}

// SetAggregationResponse sets a single aggregations payload returned by every
// AggregationSearch call.
func (m *MockOpenSearchClient) SetAggregationResponse(response any) {
	m.aggregationResponses = []json.RawMessage{mustMarshal(response)}
}

// SetAggregationResponses sets the aggregations payloads returned by
// successive AggregationSearch calls, in order.
func (m *MockOpenSearchClient) SetAggregationResponses(responses ...any) {
	m.aggregationResponses = m.aggregationResponses[:0]
	for _, response := range responses {
		m.aggregationResponses = append(m.aggregationResponses, mustMarshal(response))
	}
}

func (m *MockOpenSearchClient) SetAggregationError(err error) {
	m.aggregationError = err
}

func (m *MockOpenSearchClient) SetMappingResponse(response *IndexMapping) {
	m.mappingResponse = response
}

func (m *MockOpenSearchClient) SetMappingError(err error) {
	m.mappingError = err
}

func (m *MockOpenSearchClient) IsReady(ctx context.Context) error {
	return nil
}

func TestOpenSearchSearcherQueryResources(t *testing.T) {
	tests := []struct {
		name           string
		criteria       model.SearchCriteria
		setupMock      func(*MockOpenSearchClient)
		expectedError  bool
		expectedCount  int
		expectedErrMsg string
	}{
		{
			name: "successful search with single result",
			criteria: model.SearchCriteria{
				Name: stringPtr("test project"),
			},
			setupMock: func(mock *MockOpenSearchClient) {
				hitSource := map[string]any{
					"object_type": "project",
					"object_id":   "test-project",
					"data": map[string]any{
						"name":        "Test Project",
						"description": "Test project description",
					},
					"public": true,
				}
				sourceBytes, errMarshal := json.Marshal(hitSource)
				if errMarshal != nil {
					t.Fatalf("failed to marshal hit source: %v", errMarshal)
				}

				mock.SetSearchResponse(&SearchResponse{
					Hits: Hits{
						Total: Total{Value: 1},
						Hits: []Hit{
							{
								ID:     "test-project",
								Score:  1.0,
								Source: sourceBytes,
							},
						},
					},
				})
			},
			expectedError: false,
			expectedCount: 1,
		},
		{
			name: "successful search with multiple results",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
			},
			setupMock: func(mock *MockOpenSearchClient) {
				hits := []Hit{}
				for i := 0; i < 3; i++ {
					hitSource := map[string]any{
						"object_type": "project",
						"object_id":   fmt.Sprintf("project-%d", i),
						"data": map[string]any{
							"name": fmt.Sprintf("Project %d", i),
						},
						"public": true,
					}
					sourceBytes, errMarshal := json.Marshal(hitSource)
					if errMarshal != nil {
						t.Fatalf("failed to marshal hit source: %v", errMarshal)
					}
					hits = append(hits, Hit{
						ID:     fmt.Sprintf("project-%d", i),
						Score:  1.0,
						Source: sourceBytes,
					})
				}

				mock.SetSearchResponse(&SearchResponse{
					Hits: Hits{
						Total: Total{Value: 3},
						Hits:  hits,
					},
				})
			},
			expectedError: false,
			expectedCount: 3,
		},
		{
			name: "successful search with no results",
			criteria: model.SearchCriteria{
				Name: stringPtr("nonexistent"),
			},
			setupMock: func(mock *MockOpenSearchClient) {
				mock.SetSearchResponse(&SearchResponse{
					Hits: Hits{
						Total: Total{Value: 0},
						Hits:  []Hit{},
					},
				})
			},
			expectedError: false,
			expectedCount: 0,
		},
		{
			name: "search with client error",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			setupMock: func(mock *MockOpenSearchClient) {
				mock.SetSearchError(errors.New("connection failed"))
			},
			expectedError:  true,
			expectedErrMsg: "opensearch search failed",
		},
		{
			name: "search with invalid source data",
			criteria: model.SearchCriteria{
				Name: stringPtr("test"),
			},
			setupMock: func(mock *MockOpenSearchClient) {
				mock.SetSearchResponse(&SearchResponse{
					Hits: Hits{
						Total: Total{Value: 1},
						Hits: []Hit{
							{
								ID:     "invalid-hit",
								Score:  1.0,
								Source: []byte("invalid json"),
							},
						},
					},
				})
			},
			expectedError: false,
			expectedCount: 0, // Hit should be skipped due to invalid JSON
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockClient := NewMockOpenSearchClient()
			tc.setupMock(mockClient)

			// Create searcher
			searcher := &OpenSearchSearcher{
				client: mockClient,
				index:  "test-index",
			}

			// Execute
			ctx := context.Background()
			result, err := searcher.QueryResources(ctx, tc.criteria)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				return
			}

			assertion.NoError(err)
			assertion.NotNil(result)
			assertion.Equal(tc.expectedCount, len(result.Resources))
		})
	}
}

func TestOpenSearchSearcherRender(t *testing.T) {
	tests := []struct {
		name             string
		criteria         model.SearchCriteria
		expectedError    bool
		expectedFields   []string
		unexpectedFields []string
	}{
		{
			name: "render query with name only",
			criteria: model.SearchCriteria{
				Name: stringPtr("test project"),
			},
			expectedError:  false,
			expectedFields: []string{"multi_match", "test project"},
		},
		{
			name: "render query with resource type",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
			},
			expectedError:  false,
			expectedFields: []string{"object_type", "project"},
		},
		{
			name: "render query with tags (OR logic)",
			criteria: model.SearchCriteria{
				Tags: []string{"active", "governance"},
			},
			expectedError:  false,
			expectedFields: []string{"should", "active", "governance"},
		},
		{
			name: "render query with tags_all (AND logic)",
			criteria: model.SearchCriteria{
				TagsAll: []string{"active", "governance"},
			},
			expectedError:  false,
			expectedFields: []string{"must", "active", "governance"},
		},
		{
			name: "render query with both tags and tags_all",
			criteria: model.SearchCriteria{
				Tags:    []string{"public"},
				TagsAll: []string{"active", "governance"},
			},
			expectedError:  false,
			expectedFields: []string{"must", "should", "public", "active", "governance"},
		},
		{
			name: "render query with multiple criteria",
			criteria: model.SearchCriteria{
				Name:         stringPtr("test"),
				ResourceType: stringPtr("project"),
				Tags:         []string{"active"},
				SortBy:       "name",
				SortOrder:    "asc",
				PageSize:     10,
			},
			expectedError:  false,
			expectedFields: []string{"multi_match", "object_type", "should", "sort"},
		},
		{
			name: "render query with empty criteria",
			criteria: model.SearchCriteria{
				PageSize: 20,
			},
			expectedError:  false,
			expectedFields: []string{"size", "20"},
		},
		{
			name: "render query with filters (single field)",
			criteria: model.SearchCriteria{
				Filters: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"data.status", "active"},
		},
		{
			name: "render query with filters (multiple fields)",
			criteria: model.SearchCriteria{
				Filters: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
					{Field: "data.priority", Value: "high"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"data.status", "active", "data.priority", "high"},
		},
		{
			name: "render query with both tags_all and filters",
			criteria: model.SearchCriteria{
				TagsAll: []string{"governance"},
				Filters: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"must", "tags", "governance", "data.status", "active"},
		},
		{
			name: "render query with filters_all (single filter)",
			criteria: model.SearchCriteria{
				FiltersAll: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"must", "data.status", "active"},
		},
		{
			name: "render query with filters_all (multiple filters)",
			criteria: model.SearchCriteria{
				FiltersAll: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
					{Field: "data.priority", Value: "high"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"must", "data.status", "active", "data.priority", "high"},
		},
		{
			name: "render query with filters_or (single filter)",
			criteria: model.SearchCriteria{
				FiltersOr: []model.FieldFilter{
					{Field: "data.mailing_list_id", Value: "abc"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"should", "minimum_should_match", "data.mailing_list_id", "abc"},
		},
		{
			name: "render query with filters_or (multiple filters)",
			criteria: model.SearchCriteria{
				FiltersOr: []model.FieldFilter{
					{Field: "data.mailing_list_id", Value: "abc"},
					{Field: "data.mailing_list_id", Value: "xyz"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"should", "minimum_should_match", "data.mailing_list_id", "abc", "xyz"},
		},
		{
			name: "render query with both filters (AND) and filters_or (OR)",
			criteria: model.SearchCriteria{
				Filters: []model.FieldFilter{
					{Field: "data.status", Value: "active"},
				},
				FiltersOr: []model.FieldFilter{
					{Field: "data.mailing_list_id", Value: "abc"},
					{Field: "data.mailing_list_id", Value: "xyz"},
				},
			},
			expectedError:  false,
			expectedFields: []string{"must", "data.status", "active", "should", "minimum_should_match", "data.mailing_list_id", "abc", "xyz"},
		},
		{
			name: "render query with object_refs (filter_grants pre-filter)",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("v1_past_meeting"),
				ObjectRefs:   []string{"v1_past_meeting:meeting-1", "v1_past_meeting:meeting-2"},
			},
			expectedError:  false,
			expectedFields: []string{"terms", "object_ref", "v1_past_meeting:meeting-1", "v1_past_meeting:meeting-2"},
		},
		{
			name: "render query with single object_ref",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("v1_past_meeting"),
				ObjectRefs:   []string{"v1_past_meeting:only-one"},
			},
			expectedError:  false,
			expectedFields: []string{"terms", "object_ref", "v1_past_meeting:only-one"},
		},
		{
			name: "render query without object_refs does not include terms filter",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("v1_past_meeting"),
			},
			expectedError:  false,
			expectedFields: []string{"object_type", "v1_past_meeting"},
		},
		{
			name: "render query with best_match sorts by score",
			criteria: model.SearchCriteria{
				Name:      stringPtr("LF Products"),
				SortBy:    "_score",
				SortOrder: "desc",
				PageSize:  10,
			},
			expectedError:    false,
			expectedFields:   []string{"\"_score\"", "\"order\":\"desc\""},
			unexpectedFields: []string{"\"missing\":\"_last\""},
		},
		{
			name: "render query with name and default sort omits score",
			criteria: model.SearchCriteria{
				Name:      stringPtr("LF Products"),
				SortBy:    "sort_name",
				SortOrder: "asc",
				PageSize:  10,
			},
			expectedError:    false,
			expectedFields:   []string{"sort_name"},
			unexpectedFields: []string{"\"_score\""},
		},
		{
			name: "render query without name omits score sort",
			criteria: model.SearchCriteria{
				ResourceType: stringPtr("project"),
				SortBy:       "sort_name",
				SortOrder:    "asc",
				PageSize:     10,
			},
			expectedError:    false,
			expectedFields:   []string{"sort_name"},
			unexpectedFields: []string{"\"_score\""},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create searcher
			searcher := &OpenSearchSearcher{
				client: NewMockOpenSearchClient(),
				index:  "test-index",
			}

			// Execute
			ctx := context.Background()
			query, err := searcher.Render(ctx, tc.criteria)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				return
			}

			assertion.NoError(err)
			assertion.NotNil(query)

			queryStr := string(query)
			for _, field := range tc.expectedFields {
				assertion.Contains(queryStr, field)
			}
			for _, field := range tc.unexpectedFields {
				assertion.NotContains(queryStr, field)
			}
		})
	}
}

func TestOpenSearchSearcherConvertResponse(t *testing.T) {
	tests := []struct {
		name           string
		response       *SearchResponse
		expectedCount  int
		expectedError  bool
		expectedFields map[string]any
	}{
		{
			name: "convert response with valid hits",
			response: &SearchResponse{
				Hits: Hits{
					Total: Total{Value: 2},
					Hits: []Hit{
						{
							ID:    "project-1",
							Score: 1.0,
							Source: mustMarshal(map[string]any{
								"object_type": "project",
								"object_id":   "project-1",
								"data": map[string]any{
									"name": "Project 1",
								},
								"public": true,
							}),
						},
						{
							ID:    "project-2",
							Score: 0.8,
							Source: mustMarshal(map[string]any{
								"object_type": "project",
								"object_id":   "project-2",
								"data": map[string]any{
									"name": "Project 2",
								},
								"public": false,
							}),
						},
					},
				},
			},
			expectedCount: 2,
			expectedError: false,
			expectedFields: map[string]any{
				"type": "project",
				"id":   "project-1",
			},
		},
		{
			name: "convert response with empty hits",
			response: &SearchResponse{
				Hits: Hits{
					Total: Total{Value: 0},
					Hits:  []Hit{},
				},
			},
			expectedCount: 0,
			expectedError: false,
		},
		{
			name: "convert response with invalid JSON in hit",
			response: &SearchResponse{
				Hits: Hits{
					Total: Total{Value: 1},
					Hits: []Hit{
						{
							ID:     "invalid-hit",
							Score:  1.0,
							Source: []byte("invalid json"),
						},
					},
				},
			},
			expectedCount: 0, // Invalid hits should be skipped
			expectedError: false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create searcher
			searcher := &OpenSearchSearcher{
				client: NewMockOpenSearchClient(),
				index:  "test-index",
			}

			// Execute
			ctx := context.Background()
			result, err := searcher.convertSearchResponse(ctx, tc.response)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				return
			}

			assertion.NoError(err)
			assertion.NotNil(result)
			assertion.Equal(tc.expectedCount, len(result.Resources))

			// Check specific fields if expected
			if tc.expectedFields != nil && len(result.Resources) > 0 {
				firstResource := result.Resources[0]
				if expectedType, ok := tc.expectedFields["type"]; ok {
					assertion.Equal(expectedType, firstResource.Type)
				}
				if expectedID, ok := tc.expectedFields["id"]; ok {
					assertion.Equal(expectedID, firstResource.ID)
				}
			}
		})
	}
}

func TestOpenSearchSearcherConvertHit(t *testing.T) {
	tests := []struct {
		name          string
		hit           Hit
		expectedError bool
		expectedType  string
		expectedID    string
		expectedData  map[string]any
	}{
		{
			name: "convert hit with complete data",
			hit: Hit{
				ID:    "project-1",
				Score: 1.0,
				Source: mustMarshal(map[string]any{
					"object_type": "project",
					"object_id":   "project-1",
					"data": map[string]any{
						"name":        "Test Project",
						"description": "Test description",
					},
					"public":                true,
					"access_check_object":   "project:project-1",
					"access_check_relation": "view",
				}),
			},
			expectedError: false,
			expectedType:  "project",
			expectedID:    "project-1",
			expectedData: map[string]any{
				"name":        "Test Project",
				"description": "Test description",
			},
		},
		{
			name: "convert hit with no separate data field",
			hit: Hit{
				ID:    "project-2",
				Score: 1.0,
				Source: mustMarshal(map[string]any{
					"object_type": "project",
					"object_id":   "project-2",
					"name":        "Direct Project",
					"public":      true,
				}),
			},
			expectedError: false,
			expectedType:  "project",
			expectedID:    "project-2",
		},
		{
			name: "convert hit with invalid JSON",
			hit: Hit{
				ID:     "invalid-hit",
				Score:  1.0,
				Source: []byte("invalid json"),
			},
			expectedError: true,
			expectedID:    "invalid-hit",
		},
		{
			name: "convert hit with nil source",
			hit: Hit{
				ID:     "nil-source",
				Score:  1.0,
				Source: nil,
			},
			expectedError: false,
			expectedID:    "nil-source",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create searcher
			searcher := &OpenSearchSearcher{
				client: NewMockOpenSearchClient(),
				index:  "test-index",
			}

			// Execute
			resource, err := searcher.convertHit(tc.hit)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				return
			}

			assertion.NoError(err)
			assertion.Equal(tc.expectedID, resource.ID)

			if tc.expectedType != "" {
				assertion.Equal(tc.expectedType, resource.Type)
			}

			if tc.expectedData != nil {
				assertion.Equal(tc.expectedData, resource.Data)
			}
		})
	}
}

func TestNewSearcher(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedError  bool
		expectedErrMsg string
	}{
		{
			name: "create searcher with valid config",
			config: Config{
				URL:   "https://localhost:9200",
				Index: "test-index",
			},
			expectedError: false,
		},
		{
			name: "create searcher with empty URL",
			config: Config{
				URL:   "",
				Index: "test-index",
			},
			expectedError:  true,
			expectedErrMsg: "opensearch URL is required",
		},
		{
			name: "create searcher with empty index",
			config: Config{
				URL:   "https://localhost:9200",
				Index: "",
			},
			expectedError:  true,
			expectedErrMsg: "opensearch index is required",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			ctx := context.Background()
			searcher, err := NewSearcher(ctx, tc.config)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				assertion.Nil(searcher)
				return
			}

			assertion.NoError(err)
			assertion.NotNil(searcher)
			assertion.IsType(&OpenSearchSearcher{}, searcher)
		})
	}
}

func TestOpenSearchSearcherIntegration(t *testing.T) {
	assertion := assert.New(t)

	t.Run("end-to-end search flow", func(t *testing.T) {
		// Setup mock with realistic data
		mockClient := NewMockOpenSearchClient()

		hitSource := map[string]any{
			"object_type": "project",
			"object_id":   "integration-project",
			"data": map[string]any{
				"name":        "Integration Test Project",
				"description": "A project for integration testing",
				"tags":        []string{"testing", "integration"},
			},
			"public":                true,
			"access_check_object":   "project:integration-project",
			"access_check_relation": "view",
		}
		sourceBytes, errMarshal := json.Marshal(hitSource)
		if errMarshal != nil {
			t.Fatalf("failed to marshal hit source: %v", errMarshal)
		}

		mockClient.SetSearchResponse(&SearchResponse{
			Hits: Hits{
				Total: Total{Value: 1},
				Hits: []Hit{
					{
						ID:     "integration-project",
						Score:  1.0,
						Source: sourceBytes,
					},
				},
			},
		})

		// Create searcher
		searcher := &OpenSearchSearcher{
			client: mockClient,
			index:  "test-index",
		}

		// Execute search
		ctx := context.Background()
		criteria := model.SearchCriteria{
			Name:         stringPtr("Integration"),
			ResourceType: stringPtr("project"),
			Tags:         []string{"testing"},
			SortBy:       "name",
			SortOrder:    "asc",
			PageSize:     10,
		}

		result, err := searcher.QueryResources(ctx, criteria)

		// Verify
		assertion.NoError(err)
		assertion.NotNil(result)
		assertion.Equal(1, len(result.Resources))

		resource := result.Resources[0]
		assertion.Equal("integration-project", resource.ID)
		assertion.Equal("project", resource.Type)
		assertion.NotNil(resource.Data)

		// Verify data structure
		if data, ok := resource.Data.(map[string]any); ok {
			assertion.Equal("Integration Test Project", data["name"])
			assertion.Equal("A project for integration testing", data["description"])
		}
	})
}

func TestQueryResourcesPassesPageSizeToClient(t *testing.T) {
	tests := []struct {
		name             string
		pageSize         int
		expectedPageSize int
	}{
		{
			name:             "default page size",
			pageSize:         50,
			expectedPageSize: 50,
		},
		{
			name:             "custom page size",
			pageSize:         20,
			expectedPageSize: 20,
		},
		{
			name:             "max page size",
			pageSize:         1000,
			expectedPageSize: 1000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			mockClient := NewMockOpenSearchClient()
			mockClient.SetSearchResponse(&SearchResponse{
				Hits: Hits{
					Total: Total{Value: 0},
					Hits:  []Hit{},
				},
			})

			searcher := &OpenSearchSearcher{
				client: mockClient,
				index:  "test-index",
			}

			ctx := context.Background()
			criteria := model.SearchCriteria{
				PageSize: tc.pageSize,
			}

			_, err := searcher.QueryResources(ctx, criteria)
			assertion.NoError(err)
			assertion.Equal(tc.expectedPageSize, mockClient.lastPageSize)
		})
	}
}

// ctxAwareMappingClient fails GetMapping when the context it receives is
// already done, so a test can tell whether the caller's cancellation reached
// the read.
type ctxAwareMappingClient struct {
	*MockOpenSearchClient
}

func (c *ctxAwareMappingClient) GetMapping(ctx context.Context, index string) (*IndexMapping, error) {
	if err := ctx.Err(); err != nil {
		c.mappingCalls++
		return nil, err
	}
	return c.MockOpenSearchClient.GetMapping(ctx, index)
}

// lockedMappingClient makes the mock safe for concurrent GetMapping calls.
type lockedMappingClient struct {
	*MockOpenSearchClient
	mu sync.Mutex
}

func (l *lockedMappingClient) GetMapping(ctx context.Context, index string) (*IndexMapping, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.MockOpenSearchClient.GetMapping(ctx, index)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}

// Helper function to marshal JSON without error handling for test setup
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// publicCountBodies pins the /_count request body byte-for-byte to the output
// of the template before the count route was extended (captured from main at
// 7faf1ac). Existing callers must keep receiving the same public count.
var publicCountBodies = []struct {
	name     string
	criteria model.SearchCriteria
	expected string
}{
	{
		name:     "type only",
		criteria: model.SearchCriteria{PageSize: -1, PublicOnly: true, ResourceType: stringPtr("v1_past_meeting")},
		expected: `{"query":{"bool":{"must":[{"term":{"latest":true}},{"term":{"public":true}},{"term":{"object_type":"v1_past_meeting"}}]}}}`,
	},
	{
		name: "every criterion",
		criteria: model.SearchCriteria{
			PageSize: -1, PublicOnly: true,
			ResourceType: stringPtr("committee"), Parent: stringPtr("project:123"), Name: stringPtr("gov"),
			Tags: []string{"a", "b"}, TagsAll: []string{"c"},
			Filters:    []model.FieldFilter{{Field: "data.status", Value: "active"}},
			FiltersAll: []model.FieldFilter{{Field: "data.x", Value: "y"}},
			FiltersOr:  []model.FieldFilter{{Field: "data.m", Value: "1"}, {Field: "data.m", Value: "2"}},
			DateField:  stringPtr("data.start_time"), DateFrom: stringPtr("2026-08-01T00:00:00Z"), DateTo: stringPtr("2026-08-31T23:59:59Z"),
		},
		expected: `{"query":{"bool":{"must":[{"term":{"latest":true}},{"term":{"public":true}},{"term":{"object_type":"committee"}},{"term":{"parent_refs":"project:123"}},{"multi_match":{"query":"gov","type":"bool_prefix","fields":["name_and_aliases","name_and_aliases._2gram","name_and_aliases._3gram"]}},{"term":{"tags":"c"}},{"range":{"data.start_time":{"gte":"2026-08-01T00:00:00Z","lte":"2026-08-31T23:59:59Z"}}},{"term":{"data.status":"active"}},{"term":{"data.x":"y"}},{"bool":{"should":[{"term":{"data.m":"1"}},{"term":{"data.m":"2"}}],"minimum_should_match":1}}],"minimum_should_match":1,"should":[{"term":{"tags":"a"}},{"term":{"tags":"b"}}]}}}`,
	},
}

func TestOpenSearchSearcherRenderPublicCountIsUnchanged(t *testing.T) {
	searcher := &OpenSearchSearcher{client: NewMockOpenSearchClient(), index: "test-index"}
	for _, tc := range publicCountBodies {
		t.Run(tc.name, func(t *testing.T) {
			query, err := searcher.Render(context.Background(), tc.criteria)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, string(query))
		})
	}
}

func TestOpenSearchSearcherRenderCountAggregation(t *testing.T) {
	searcher := &OpenSearchSearcher{client: NewMockOpenSearchClient(), index: "test-index", accessKeyField: accessCheckQueryField}
	private := model.SearchCriteria{PrivateOnly: true, ResourceType: stringPtr("v1_past_meeting"), Tags: []string{"x"}}
	plain := model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting")}

	tests := []struct {
		name     string
		params   countAggregationParams
		expected string
	}{
		{
			name:     "access walk first page",
			params:   countAggregationParams{Criteria: private, AccessKeyField: accessCheckQueryField, AccessWalk: true, PageSize: 100},
			expected: `{"size":0,"query":{"bool":{"must":[{"term":{"latest":true}},{"bool":{"must_not":{"term":{"public":true}}}},{"term":{"object_type":"v1_past_meeting"}}],"minimum_should_match":1,"should":[{"term":{"tags":"x"}}]}},"aggs":{"access_keys":{"composite":{"size":100,"sources":[{"access_key":{"terms":{"field":"access_check_query"}}}]}}}}`,
		},
		{
			name:     "access walk later page carries after",
			params:   countAggregationParams{Criteria: private, AccessKeyField: accessCheckQueryKeywordField, AccessWalk: true, PageSize: 2, After: "v1_past_meeting:m2#viewer"},
			expected: `{"size":0,"query":{"bool":{"must":[{"term":{"latest":true}},{"bool":{"must_not":{"term":{"public":true}}}},{"term":{"object_type":"v1_past_meeting"}}],"minimum_should_match":1,"should":[{"term":{"tags":"x"}}]}},"aggs":{"access_keys":{"composite":{"size":2,"sources":[{"access_key":{"terms":{"field":"access_check_query.keyword"}}}],"after":{"access_key":"v1_past_meeting:m2#viewer"}}}}}`,
		},
		{
			name: "grouped count over public plus granted keys",
			params: countAggregationParams{
				Criteria: plain, AccessKeyField: accessCheckQueryField,
				AuthorizedFilter: true, IncludePublic: true, AuthorizedKeys: []string{"k1", "k2"},
				GroupByPrefix: "project_uid", GroupBySize: 100, GroupByInclude: "project_uid:.*",
			},
			expected: `{"size":0,"query":{"bool":{"must":[{"term":{"latest":true}},{"term":{"object_type":"v1_past_meeting"}}],"filter":{"bool":{"should":[{"term":{"public":true}},{"terms":{"access_check_query":["k1","k2"]}}],"minimum_should_match":1}}}},"aggs":{"group_by":{"terms":{"field":"tags","size":100,"include":"project_uid:.*"}}}}`,
		},
		{
			name: "grouped count for anonymous is public only",
			params: countAggregationParams{
				Criteria: plain, AccessKeyField: accessCheckQueryField,
				AuthorizedFilter: true, IncludePublic: true,
				GroupByPrefix: "meeting_type", GroupBySize: 1, GroupByInclude: "meeting_type:.*",
			},
			expected: `{"size":0,"query":{"bool":{"must":[{"term":{"latest":true}},{"term":{"object_type":"v1_past_meeting"}}],"filter":{"bool":{"should":[{"term":{"public":true}}],"minimum_should_match":1}}}},"aggs":{"group_by":{"terms":{"field":"tags","size":1,"include":"meeting_type:.*"}}}}`,
		},
		{
			name: "cardinality walk first page starts after the bare prefix",
			params: countAggregationParams{
				Criteria: plain, AccessKeyField: accessCheckQueryField,
				AuthorizedFilter: true, AuthorizedKeys: []string{"k1"},
				CardinalityPrefix: "email", PageSize: 100, After: "email:",
			},
			expected: `{"size":0,"query":{"bool":{"must":[{"term":{"latest":true}},{"term":{"object_type":"v1_past_meeting"}}],"filter":{"bool":{"should":[{"terms":{"access_check_query":["k1"]}}],"minimum_should_match":1}}}},"aggs":{"tags":{"composite":{"size":100,"sources":[{"tag":{"terms":{"field":"tags"}}}],"after":{"tag":"email:"}}}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, err := searcher.RenderCountAggregation(context.Background(), tc.params)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, string(query))
		})
	}
}

func TestTagPrefixInclude(t *testing.T) {
	assert.Equal(t, "project_uid:.*", tagPrefixInclude("project_uid"))
	// Defensive escaping of Lucene operators, even though the API pattern excludes them.
	assert.Equal(t, `a\.b\*:.*`, tagPrefixInclude("a.b*"))
}

func TestResolveAccessKeyField(t *testing.T) {
	tests := []struct {
		name     string
		mapping  *IndexMapping
		err      error
		expected string
	}{
		{
			name:     "keyword mapping uses the field itself",
			mapping:  &IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}},
			expected: accessCheckQueryField,
		},
		{
			name: "text with keyword subfield uses the subfield",
			mapping: &IndexMapping{Properties: map[string]FieldMapping{
				"access_check_query": {Type: "text", Fields: map[string]FieldMapping{"keyword": {Type: "keyword"}}},
			}},
			expected: accessCheckQueryKeywordField,
		},
		{
			name:     "text without keyword subfield falls back to the subfield with a warning",
			mapping:  &IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "text"}}},
			expected: accessCheckQueryKeywordField,
		},
		{
			name:     "missing field falls back",
			mapping:  &IndexMapping{Properties: map[string]FieldMapping{"tags": {Type: "keyword"}}},
			expected: accessCheckQueryKeywordField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewMockOpenSearchClient()
			client.SetMappingResponse(tc.mapping)
			searcher := &OpenSearchSearcher{client: client, index: "test-index"}

			assert.Equal(t, tc.expected, searcher.resolveAccessKeyField(context.Background()))
			// A successful read is memoized.
			assert.Equal(t, tc.expected, searcher.resolveAccessKeyField(context.Background()))
			assert.Equal(t, 1, client.mappingCalls)
		})
	}

	t.Run("mapping call failure falls back, retries after the interval, and is never memoized", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetMappingError(errors.New("boom"))
		clock := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
		searcher := &OpenSearchSearcher{client: client, index: "test-index", now: func() time.Time { return clock }}

		// First failure: one call, default field, retry window opens.
		assert.Equal(t, accessCheckQueryKeywordField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, 1, client.mappingCalls)

		// Inside the window: no network call, still the default.
		clock = clock.Add(accessKeyFieldRetryInterval / 2)
		assert.Equal(t, accessCheckQueryKeywordField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, 1, client.mappingCalls, "a persistent failure must not cost a call per request")

		// After the window: retried, still failing, window reopens.
		clock = clock.Add(accessKeyFieldRetryInterval)
		assert.Equal(t, accessCheckQueryKeywordField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, 2, client.mappingCalls, "a failure must not be memoized")

		// Once the mapping is readable the real field is resolved and kept.
		clock = clock.Add(accessKeyFieldRetryInterval)
		client.SetMappingError(nil)
		client.SetMappingResponse(&IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}})
		assert.Equal(t, accessCheckQueryField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, accessCheckQueryField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, 3, client.mappingCalls)
	})

	t.Run("a cancelled caller context does not poison the retry window", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetMappingResponse(&IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}})
		// The mock ignores ctx, so make cancellation observable: fail only
		// when the context handed to GetMapping is already done.
		searcher := &OpenSearchSearcher{client: &ctxAwareMappingClient{MockOpenSearchClient: client}, index: "test-index"}

		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		assert.Equal(t, accessCheckQueryField, searcher.resolveAccessKeyField(cancelled),
			"the read runs on a context decoupled from the caller, so it succeeds")
		assert.Equal(t, 1, client.mappingCalls)
	})

	t.Run("concurrent first use resolves once", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetMappingResponse(&IndexMapping{Properties: map[string]FieldMapping{"access_check_query": {Type: "keyword"}}})
		searcher := &OpenSearchSearcher{client: &lockedMappingClient{MockOpenSearchClient: client}, index: "test-index"}

		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				assert.Equal(t, accessCheckQueryField, searcher.resolveAccessKeyField(context.Background()))
			}()
		}
		wg.Wait()
		// The mapping call runs outside the searcher lock, so several requests
		// may race it on first use; every one of them must agree afterwards,
		// and it must not be called again.
		before := client.mappingCalls
		assert.Equal(t, accessCheckQueryField, searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, before, client.mappingCalls)
	})

	t.Run("preset field skips the mapping call", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: "preset"}
		assert.Equal(t, "preset", searcher.resolveAccessKeyField(context.Background()))
		assert.Equal(t, 0, client.mappingCalls)
	})
}

func TestOpenSearchSearcherCountPublic(t *testing.T) {
	t.Run("returns the count", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetCountResponse(&CountResponse{Count: 5})
		searcher := &OpenSearchSearcher{client: client, index: "test-index"}
		count, err := searcher.CountPublic(context.Background(), model.SearchCriteria{PublicOnly: true, PageSize: -1})
		assert.NoError(t, err)
		assert.Equal(t, 5, count)
	})
	t.Run("propagates count errors", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetCountError(errors.New("opensearch count failed"))
		searcher := &OpenSearchSearcher{client: client, index: "test-index"}
		_, err := searcher.CountPublic(context.Background(), model.SearchCriteria{PublicOnly: true, PageSize: -1})
		assert.Error(t, err)
	})
	t.Run("rejects non-public criteria", func(t *testing.T) {
		searcher := &OpenSearchSearcher{client: NewMockOpenSearchClient(), index: "test-index"}
		_, err := searcher.CountPublic(context.Background(), model.SearchCriteria{})
		assert.Error(t, err)
	})
}

func TestOpenSearchSearcherAccessBuckets(t *testing.T) {
	private := model.SearchCriteria{PrivateOnly: true, ResourceType: stringPtr("v1_past_meeting")}

	t.Run("converts composite buckets and the after key", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationResponse(map[string]any{
			"access_keys": map[string]any{
				"after_key": map[string]string{"access_key": "v1_past_meeting:m3#viewer"},
				"buckets": []map[string]any{
					{"key": map[string]string{"access_key": "v1_past_meeting:m2#viewer"}, "doc_count": 1},
					{"key": map[string]string{"access_key": "v1_past_meeting:m3#viewer"}, "doc_count": 4},
				},
			},
		})
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		page, err := searcher.AccessBuckets(context.Background(), private, model.AccessBucketRequest{PageSize: 2})
		assert.NoError(t, err)
		assert.Equal(t, []model.AggregationBucket{
			{Key: "v1_past_meeting:m2#viewer", DocCount: 1},
			{Key: "v1_past_meeting:m3#viewer", DocCount: 4},
		}, page.Buckets)
		assert.NotNil(t, page.AfterKey)
		assert.Equal(t, "v1_past_meeting:m3#viewer", *page.AfterKey)
	})

	t.Run("last page has no after key", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationResponse(map[string]any{"access_keys": map[string]any{"buckets": []map[string]any{}}})
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		page, err := searcher.AccessBuckets(context.Background(), private, model.AccessBucketRequest{PageSize: 2})
		assert.NoError(t, err)
		assert.Empty(t, page.Buckets)
		assert.Nil(t, page.AfterKey)
	})

	t.Run("missing aggregation on a 200 is an error, not zero buckets", func(t *testing.T) {
		// A silent zero here is exactly what the mapping resolution guards
		// against; the response shape must be present.
		client := NewMockOpenSearchClient()
		client.SetAggregationResponse(map[string]any{})
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		_, err := searcher.AccessBuckets(context.Background(), private, model.AccessBucketRequest{PageSize: 2})
		assert.Error(t, err)
	})

	t.Run("propagates search errors", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationError(errors.New("opensearch aggregation failed"))
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}
		_, err := searcher.AccessBuckets(context.Background(), private, model.AccessBucketRequest{PageSize: 2})
		assert.Error(t, err)
	})

	t.Run("rejects non-private criteria", func(t *testing.T) {
		searcher := &OpenSearchSearcher{client: NewMockOpenSearchClient(), index: "test-index", accessKeyField: accessCheckQueryField}
		_, err := searcher.AccessBuckets(context.Background(), model.SearchCriteria{}, model.AccessBucketRequest{PageSize: 2})
		assert.Error(t, err)
	})
}

func TestGroupByStripsPrefix(t *testing.T) {
	client := NewMockOpenSearchClient()
	client.SetAggregationResponse(map[string]any{
		"group_by": map[string]any{
			"doc_count_error_upper_bound": 0,
			"sum_other_doc_count":         3,
			"buckets": []map[string]any{
				{"key": "project_uid:P1", "doc_count": 2},
				{"key": "project_uid:P2", "doc_count": 2},
			},
		},
	})
	searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

	result, err := searcher.AuthorizedAggregation(context.Background(),
		model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting")},
		model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 2, IncludePublic: true, AuthorizedKeys: []string{"k"}},
	)
	assert.NoError(t, err)
	assert.Equal(t, []model.CountGroup{{Key: "P1", Count: 2}, {Key: "P2", Count: 2}}, result.Groups)
	assert.False(t, result.GroupsComplete, "sum_other_doc_count > 0 means more groups exist")
	assert.Equal(t, 1, client.aggregationCalls)
	assert.Contains(t, string(client.aggregationQueries[0]), `"include":"project_uid:.*"`)
}

func TestCardinalityWalkStopsAtPrefixBoundary(t *testing.T) {
	compositePage := func(after string, keys ...string) map[string]any {
		buckets := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			buckets = append(buckets, map[string]any{"key": map[string]string{"tag": key}, "doc_count": 1})
		}
		page := map[string]any{"buckets": buckets}
		if after != "" {
			page["after_key"] = map[string]string{"tag": after}
		}
		return map[string]any{"tags": page}
	}
	criteria := model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting_participant")}
	aggregation := func(pageSize, maxDistinct int) model.CountAggregation {
		return model.CountAggregation{CardinalityPrefix: "email", IncludePublic: true, PageSize: pageSize, MaxDistinct: maxDistinct}
	}

	t.Run("stops at the first key outside the prefix; emailx and emai are not counted", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		// Ascending key order: "emai:bar" < "email:" (the walk starts after
		// "email:" so it is never returned), "emailx:foo" > every "email:…".
		client.SetAggregationResponses(
			compositePage("emailx:foo", "email:a@x.org", "email:b@y.org", "emailx:foo"),
		)
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		result, err := searcher.AuthorizedAggregation(context.Background(), criteria, aggregation(3, 5000))
		assert.NoError(t, err)
		assert.Equal(t, uint64(2), result.MetricValue)
		assert.True(t, result.MetricComplete)
		assert.Equal(t, 1, client.aggregationCalls)
		assert.Contains(t, string(client.aggregationQueries[0]), `"after":{"tag":"email:"}`)
	})

	t.Run("pages until a short page and carries the after key", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationResponses(
			compositePage("email:b@y.org", "email:a@x.org", "email:b@y.org"),
			compositePage("email:c@z.org", "email:c@z.org"),
		)
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		result, err := searcher.AuthorizedAggregation(context.Background(), criteria, aggregation(2, 5000))
		assert.NoError(t, err)
		assert.Equal(t, uint64(3), result.MetricValue)
		assert.True(t, result.MetricComplete)
		assert.Equal(t, 2, client.aggregationCalls)
		assert.Contains(t, string(client.aggregationQueries[1]), `"after":{"tag":"email:b@y.org"}`)
	})

	t.Run("cap stops the walk and flags the metric incomplete", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationResponses(
			compositePage("email:b@y.org", "email:a@x.org", "email:b@y.org"),
			compositePage("email:d@z.org", "email:c@z.org", "email:d@z.org"),
		)
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		result, err := searcher.AuthorizedAggregation(context.Background(), criteria, aggregation(2, 3))
		assert.NoError(t, err)
		assert.Equal(t, uint64(3), result.MetricValue)
		assert.False(t, result.MetricComplete)
		assert.Equal(t, 2, client.aggregationCalls)
	})

	t.Run("no matching tags is zero and complete", func(t *testing.T) {
		client := NewMockOpenSearchClient()
		client.SetAggregationResponses(compositePage(""))
		searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

		result, err := searcher.AuthorizedAggregation(context.Background(), criteria, aggregation(100, 5000))
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), result.MetricValue)
		assert.True(t, result.MetricComplete)
	})
}

func TestAuthorizedAggregationSkipsWhenNothingIsVisible(t *testing.T) {
	client := NewMockOpenSearchClient()
	searcher := &OpenSearchSearcher{client: client, index: "test-index", accessKeyField: accessCheckQueryField}

	t.Run("no public, no granted keys: no query", func(t *testing.T) {
		result, err := searcher.AuthorizedAggregation(context.Background(), model.SearchCriteria{}, model.CountAggregation{GroupByPrefix: "x", GroupBySize: 10})
		assert.NoError(t, err)
		assert.Empty(t, result.Groups)
		assert.True(t, result.GroupsComplete)
		assert.Equal(t, 0, client.aggregationCalls)
	})

	t.Run("nothing requested: no query", func(t *testing.T) {
		result, err := searcher.AuthorizedAggregation(context.Background(), model.SearchCriteria{}, model.CountAggregation{IncludePublic: true})
		assert.NoError(t, err)
		assert.Empty(t, result.Groups)
		assert.Equal(t, 0, client.aggregationCalls)
	})
}
