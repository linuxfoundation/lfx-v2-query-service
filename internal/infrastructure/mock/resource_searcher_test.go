// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
)

func TestMockResourceSearcherQueryResourcesCount(t *testing.T) {
	tests := []struct {
		name                string
		countCriteria       model.SearchCriteria
		aggregationCriteria model.SearchCriteria
		publicOnly          bool
		expectedCount       int
		expectedError       bool
	}{
		{
			name: "count all resources",
			countCriteria: model.SearchCriteria{
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       5, // Total resources in mock data
			expectedError:       false,
		},
		{
			name: "count public only resources",
			countCriteria: model.SearchCriteria{
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          true,
			expectedCount:       1, // Only one public resource in mock data
			expectedError:       false,
		},
		{
			name: "count resources by type",
			countCriteria: model.SearchCriteria{
				ResourceType: stringPtr("committee"),
				PageSize:     -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       2, // Two committees in mock data
			expectedError:       false,
		},
		{
			name: "count resources by name",
			countCriteria: model.SearchCriteria{
				Name:     stringPtr("Security"),
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       2, // Resources containing "Security" in name
			expectedError:       false,
		},
		{
			name: "count resources by tags",
			countCriteria: model.SearchCriteria{
				Tags:     []string{"active"},
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       5, // All resources have "active" tag
			expectedError:       false,
		},
		{
			name: "count resources by tags_all (AND logic)",
			countCriteria: model.SearchCriteria{
				TagsAll:  []string{"active", "security"},
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       2, // Resources with both "active" AND "security" tags
			expectedError:       false,
		},
		{
			name: "count resources by tags (OR logic)",
			countCriteria: model.SearchCriteria{
				Tags:     []string{"governance", "security"},
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       4, // Resources with "governance" OR "security" tags
			expectedError:       false,
		},
		{
			name: "count resources with both tags and tags_all",
			countCriteria: model.SearchCriteria{
				Tags:     []string{"public"},
				TagsAll:  []string{"active", "security"},
				PageSize: -1,
			},
			aggregationCriteria: model.SearchCriteria{},
			publicOnly:          false,
			expectedCount:       0, // Resources with (public) AND (active AND security) - no matches in test data
			expectedError:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			// Create mock searcher
			searcher := NewMockResourceSearcher()

			// Execute
			ctx := context.Background()
			result, err := searcher.QueryResourcesCount(ctx, tc.countCriteria, tc.aggregationCriteria, tc.publicOnly)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
			} else {
				assertion.NoError(err)
				assertion.NotNil(result)
				assertion.Equal(tc.expectedCount, result.Count)
				assertion.NotNil(result.Aggregation)
				assertion.False(result.HasMore) // Mock always returns false for HasMore
			}
		})
	}
}

func TestMockResourceSearcherQueryResourcesCountWithAggregation(t *testing.T) {
	assertion := assert.New(t)

	// Create mock searcher
	searcher := NewMockResourceSearcher()

	// Test aggregation by resource type
	countCriteria := model.SearchCriteria{
		PageSize: -1,
	}
	aggregationCriteria := model.SearchCriteria{
		ResourceType: stringPtr(""),
	}

	ctx := context.Background()
	result, err := searcher.QueryResourcesCount(ctx, countCriteria, aggregationCriteria, false)

	assertion.NoError(err)
	assertion.NotNil(result)
	assertion.Equal(5, result.Count) // Total count
	assertion.NotNil(result.Aggregation)
	assertion.Greater(len(result.Aggregation.Buckets), 0) // Should have aggregation buckets

	// Verify aggregation buckets contain expected types
	bucketKeys := make([]string, len(result.Aggregation.Buckets))
	for i, bucket := range result.Aggregation.Buckets {
		bucketKeys[i] = bucket.Key
	}
	assertion.Contains(bucketKeys, "committee")
	assertion.Contains(bucketKeys, "project")
	assertion.Contains(bucketKeys, "meeting")
}

func TestMockResourceSearcherQueryResourcesWithTags(t *testing.T) {
	tests := []struct {
		name          string
		criteria      model.SearchCriteria
		expectedCount int
		expectedError bool
	}{
		{
			name: "search with tags (OR logic)",
			criteria: model.SearchCriteria{
				Tags: []string{"governance", "security"},
			},
			expectedCount: 4, // Resources with "governance" OR "security"
			expectedError: false,
		},
		{
			name: "search with tags_all (AND logic)",
			criteria: model.SearchCriteria{
				TagsAll: []string{"active", "security"},
			},
			expectedCount: 2, // Resources with both "active" AND "security"
			expectedError: false,
		},
		{
			name: "search with tags_all (AND logic) - all three tags",
			criteria: model.SearchCriteria{
				TagsAll: []string{"active", "security", "private"},
			},
			expectedCount: 1, // Only one resource has all three tags
			expectedError: false,
		},
		{
			name: "search with tags_all (AND logic) - no matches",
			criteria: model.SearchCriteria{
				TagsAll: []string{"governance", "platform"},
			},
			expectedCount: 0, // No resources have both tags
			expectedError: false,
		},
		{
			name: "search with both tags and tags_all",
			criteria: model.SearchCriteria{
				Tags:    []string{"governance"},
				TagsAll: []string{"active", "security"},
			},
			expectedCount: 0, // Resources must have (governance) AND (active AND security) - no match
			expectedError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)

			// Create mock searcher
			searcher := NewMockResourceSearcher()

			// Execute
			ctx := context.Background()
			result, err := searcher.QueryResources(ctx, tc.criteria)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Nil(result)
			} else {
				assertion.NoError(err)
				assertion.NotNil(result)
				assertion.Equal(tc.expectedCount, len(result.Resources))
			}
		})
	}
}

func TestMockResourceSearcherAddResource(t *testing.T) {
	assertion := assert.New(t)

	// Create mock searcher
	searcher := NewMockResourceSearcher()
	initialCount := searcher.GetResourceCount()

	// Add a new resource
	newResource := NewResourceWithDefaults("test-type", "test-id", map[string]any{
		"name": "Test Resource",
	}, true)

	searcher.AddResource(newResource)

	// Verify count increased
	assertion.Equal(initialCount+1, searcher.GetResourceCount())

	// Verify the resource can be found
	ctx := context.Background()
	result, err := searcher.QueryResources(ctx, model.SearchCriteria{
		ResourceType: stringPtr("test-type"),
	})

	assertion.NoError(err)
	assertion.Equal(1, len(result.Resources))
	assertion.Equal("test-id", result.Resources[0].ID)
	assertion.Equal("test-type", result.Resources[0].Type)
}

func TestMockResourceSearcherClearResources(t *testing.T) {
	assertion := assert.New(t)

	// Create mock searcher
	searcher := NewMockResourceSearcher()
	assertion.Greater(searcher.GetResourceCount(), 0)

	// Clear resources
	searcher.ClearResources()

	// Verify count is zero
	assertion.Equal(0, searcher.GetResourceCount())

	// Verify search returns empty
	ctx := context.Background()
	result, err := searcher.QueryResources(ctx, model.SearchCriteria{})

	assertion.NoError(err)
	assertion.Equal(0, len(result.Resources))
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
