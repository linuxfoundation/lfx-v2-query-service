// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain"
	"github.com/stretchr/testify/assert"
)

// MockOpenSearchClient is a mock implementation of OpenSearchClientRetriever
type MockOpenSearchClient struct {
	searchResponse *SearchResponse
	searchError    error
}

func NewMockOpenSearchClient() *MockOpenSearchClient {
	return &MockOpenSearchClient{}
}

func (m *MockOpenSearchClient) Search(ctx context.Context, index string, query []byte) (*SearchResponse, error) {
	if m.searchError != nil {
		return nil, m.searchError
	}
	return m.searchResponse, nil
}

func (m *MockOpenSearchClient) SetSearchResponse(response *SearchResponse) {
	m.searchResponse = response
}

func (m *MockOpenSearchClient) SetSearchError(err error) {
	m.searchError = err
}

func (m *MockOpenSearchClient) IsReady(ctx context.Context) error {
	return nil
}

func TestOpenSearchSearcherQueryResources(t *testing.T) {
	tests := []struct {
		name           string
		criteria       domain.SearchCriteria
		setupMock      func(*MockOpenSearchClient)
		expectedError  bool
		expectedCount  int
		expectedErrMsg string
	}{
		{
			name: "successful search with single result",
			criteria: domain.SearchCriteria{
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
			criteria: domain.SearchCriteria{
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
			criteria: domain.SearchCriteria{
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
			criteria: domain.SearchCriteria{
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
			criteria: domain.SearchCriteria{
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
		name           string
		criteria       domain.SearchCriteria
		expectedError  bool
		expectedFields []string
	}{
		{
			name: "render query with name only",
			criteria: domain.SearchCriteria{
				Name: stringPtr("test project"),
			},
			expectedError:  false,
			expectedFields: []string{"multi_match", "test project"},
		},
		{
			name: "render query with resource type",
			criteria: domain.SearchCriteria{
				ResourceType: stringPtr("project"),
			},
			expectedError:  false,
			expectedFields: []string{"object_type", "project"},
		},
		{
			name: "render query with tags",
			criteria: domain.SearchCriteria{
				Tags: []string{"active", "governance"},
			},
			expectedError:  false,
			expectedFields: []string{"should", "active", "governance"},
		},
		{
			name: "render query with multiple criteria",
			criteria: domain.SearchCriteria{
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
			criteria: domain.SearchCriteria{
				PageSize: 20,
			},
			expectedError:  false,
			expectedFields: []string{"size", "20"},
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
			result, err := searcher.convertResponse(ctx, tc.response)

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
		criteria := domain.SearchCriteria{
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
