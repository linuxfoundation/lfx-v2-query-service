// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
)

func TestMockResourceSearcherCountPublic(t *testing.T) {
	tests := []struct {
		name          string
		criteria      model.SearchCriteria
		setup         func(*MockResourceSearcher)
		expectedCount int
		expectedError bool
	}{
		{
			name:          "counts public resources only",
			criteria:      model.SearchCriteria{PublicOnly: true},
			expectedCount: 1,
		},
		{
			name:          "type filter applies",
			criteria:      model.SearchCriteria{PublicOnly: true, ResourceType: stringPtr("committee")},
			expectedCount: 0,
		},
		{
			name:          "forced response wins",
			criteria:      model.SearchCriteria{PublicOnly: true},
			setup:         func(m *MockResourceSearcher) { m.SetCountPublicResponse(42) },
			expectedCount: 42,
		},
		{
			name:          "forced error wins",
			criteria:      model.SearchCriteria{PublicOnly: true},
			setup:         func(m *MockResourceSearcher) { m.SetCountPublicError(assert.AnError) },
			expectedError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)
			searcher := NewMockResourceSearcher()
			if tc.setup != nil {
				tc.setup(searcher)
			}
			count, err := searcher.CountPublic(context.Background(), tc.criteria)
			if tc.expectedError {
				assertion.Error(err)
				return
			}
			assertion.NoError(err)
			assertion.Equal(tc.expectedCount, count)
		})
	}
}

func TestMockResourceSearcherAccessBuckets(t *testing.T) {
	assertion := assert.New(t)
	ctx := context.Background()

	t.Run("groups private resources by access key in key order and pages", func(t *testing.T) {
		searcher := NewMockResourceSearcher()
		criteria := model.SearchCriteria{PrivateOnly: true}

		page1, err := searcher.AccessBuckets(ctx, criteria, model.AccessBucketRequest{PageSize: 2})
		assertion.NoError(err)
		assertion.Len(page1.Buckets, 2)
		assertion.Equal("committee:123#member", page1.Buckets[0].Key)
		assertion.Equal(uint64(1), page1.Buckets[0].DocCount)
		assertion.Equal("committee:567#member", page1.Buckets[1].Key)
		assertion.NotNil(page1.AfterKey)

		page2, err := searcher.AccessBuckets(ctx, criteria, model.AccessBucketRequest{PageSize: 2, After: page1.AfterKey})
		assertion.NoError(err)
		// The private resource with empty access fields has no key and is skipped.
		assertion.Len(page2.Buckets, 1)
		assertion.Equal("project:789#contributor", page2.Buckets[0].Key)
		assertion.Equal(2, searcher.AccessBucketCalls())
	})

	t.Run("forced pages are returned in order and the last repeats", func(t *testing.T) {
		searcher := NewMockResourceSearcher()
		first := &model.AccessBucketPage{Buckets: []model.AggregationBucket{{Key: "a", DocCount: 1}}}
		second := &model.AccessBucketPage{Buckets: []model.AggregationBucket{}}
		searcher.SetAccessBucketPages(first, second)

		p, _ := searcher.AccessBuckets(ctx, model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 1})
		assertion.Equal(first, p)
		p, _ = searcher.AccessBuckets(ctx, model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 1})
		assertion.Equal(second, p)
		p, _ = searcher.AccessBuckets(ctx, model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 1})
		assertion.Equal(second, p)
	})

	t.Run("forced error wins", func(t *testing.T) {
		searcher := NewMockResourceSearcher()
		searcher.SetAccessBucketsError(assert.AnError)
		_, err := searcher.AccessBuckets(ctx, model.SearchCriteria{PrivateOnly: true}, model.AccessBucketRequest{PageSize: 1})
		assertion.Error(err)
	})
}

func TestMockResourceSearcherAuthorizedAggregation(t *testing.T) {
	assertion := assert.New(t)
	ctx := context.Background()

	newSearcher := func() *MockResourceSearcher {
		searcher := NewMockResourceSearcher()
		searcher.ClearResources()
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting", "m1", map[string]any{"tags": []string{"project_uid:P1", "meeting_type:recurring"}}, true))
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting", "m2", map[string]any{"tags": []string{"project_uid:P1", "meeting_type:single"}}, false))
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting", "m3", map[string]any{"tags": []string{"project_uid:P2", "meeting_type:recurring"}}, false))
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting_participant", "p1", map[string]any{"tags": []string{"email:a@x.org"}}, false))
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting_participant", "p2", map[string]any{"tags": []string{"email:a@x.org"}}, false))
		searcher.AddResource(NewResourceWithDefaults("v1_past_meeting_participant", "p3", map[string]any{"tags": []string{"email:b@y.org", "emailx:zzz"}}, false))
		return searcher
	}

	t.Run("groups over public plus granted resources, count desc then key asc", func(t *testing.T) {
		searcher := newSearcher()
		result, err := searcher.AuthorizedAggregation(ctx,
			model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting")},
			model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100, IncludePublic: true, AuthorizedKeys: []string{"v1_past_meeting:m2#viewer", "v1_past_meeting:m3#viewer"}},
		)
		assertion.NoError(err)
		assertion.True(result.GroupsComplete)
		assertion.Equal([]model.CountGroup{{Key: "P1", Count: 2}, {Key: "P2", Count: 1}}, result.Groups)
	})

	t.Run("denied private resources are excluded", func(t *testing.T) {
		searcher := newSearcher()
		result, err := searcher.AuthorizedAggregation(ctx,
			model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting")},
			model.CountAggregation{GroupByPrefix: "project_uid", GroupBySize: 100, IncludePublic: true},
		)
		assertion.NoError(err)
		assertion.Equal([]model.CountGroup{{Key: "P1", Count: 1}}, result.Groups)
	})

	t.Run("group_by_size truncates and flags incompleteness", func(t *testing.T) {
		searcher := newSearcher()
		result, err := searcher.AuthorizedAggregation(ctx,
			model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting")},
			model.CountAggregation{GroupByPrefix: "meeting_type", GroupBySize: 1, IncludePublic: true, AuthorizedKeys: []string{"v1_past_meeting:m2#viewer", "v1_past_meeting:m3#viewer"}},
		)
		assertion.NoError(err)
		assertion.False(result.GroupsComplete)
		assertion.Equal([]model.CountGroup{{Key: "recurring", Count: 2}}, result.Groups)
	})

	t.Run("cardinality counts distinct values with an exact prefix boundary", func(t *testing.T) {
		searcher := newSearcher()
		result, err := searcher.AuthorizedAggregation(ctx,
			model.SearchCriteria{ResourceType: stringPtr("v1_past_meeting_participant")},
			model.CountAggregation{CardinalityPrefix: "email", AuthorizedKeys: []string{"v1_past_meeting_participant:p1#viewer", "v1_past_meeting_participant:p2#viewer", "v1_past_meeting_participant:p3#viewer"}},
		)
		assertion.NoError(err)
		assertion.True(result.MetricComplete)
		assertion.Equal(uint64(2), result.MetricValue)
	})

	t.Run("nothing requested returns the empty result", func(t *testing.T) {
		searcher := newSearcher()
		result, err := searcher.AuthorizedAggregation(ctx, model.SearchCriteria{}, model.CountAggregation{})
		assertion.NoError(err)
		assertion.Empty(result.Groups)
		assertion.Equal(uint64(0), result.MetricValue)
	})

	t.Run("forced response and error win", func(t *testing.T) {
		searcher := newSearcher()
		forced := &model.CountAggregationResult{MetricValue: 7}
		searcher.SetAuthorizedAggregationResponse(forced)
		result, err := searcher.AuthorizedAggregation(ctx, model.SearchCriteria{}, model.CountAggregation{CardinalityPrefix: "x"})
		assertion.NoError(err)
		assertion.Equal(forced, result)

		searcher.SetAuthorizedAggregationError(assert.AnError)
		_, err = searcher.AuthorizedAggregation(ctx, model.SearchCriteria{}, model.CountAggregation{CardinalityPrefix: "x"})
		assertion.Error(err)
	})
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
