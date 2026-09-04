// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// MockResourceSearcher is a mock implementation of ResourceSearcher for testing
// This demonstrates how the clean architecture allows easy swapping of implementations
type MockResourceSearcher struct {
	resources                     []model.Resource
	countPublicResponse           *int
	countPublicError              error
	accessBucketPages             []*model.AccessBucketPage
	accessBucketCalls             int
	accessBucketsError            error
	authorizedAggregationResponse *model.CountAggregationResult
	authorizedAggregationError    error
	isReadyError                  error
}

// NewMockResourceSearcher creates a new mock searcher with some sample data
func NewMockResourceSearcher() *MockResourceSearcher {
	return &MockResourceSearcher{
		resources: []model.Resource{
			{
				Type: "committee",
				ID:   "123",
				Data: map[string]any{
					"name":        "Technical Advisory Committee",
					"description": "Main technical governance body",
					"status":      "active",
					"tags":        []string{"active", "governance"},
				},
				TransactionBodyStub: model.TransactionBodyStub{
					ObjectRef:            "committee:123",
					ObjectType:           "committee",
					ObjectID:             "123",
					Public:               false,
					AccessCheckObject:    "committee:123",
					AccessCheckRelation:  "member",
					HistoryCheckObject:   "committee:123",
					HistoryCheckRelation: "viewer",
				},
				NeedCheck: true,
			},
			{
				Type: "project",
				ID:   "456",
				Data: map[string]any{
					"name":        "LFX Platform Project",
					"slug":        "lfx-platform-project",
					"description": "Core platform development project",
					"status":      "active",
					"tags":        []string{"active", "platform"},
				},
				TransactionBodyStub: model.TransactionBodyStub{
					ObjectRef:            "project:456",
					ObjectType:           "project",
					ObjectID:             "456",
					Public:               true,
					AccessCheckObject:    "project:456",
					AccessCheckRelation:  "viewer",
					HistoryCheckObject:   "project:456",
					HistoryCheckRelation: "viewer",
				},
				NeedCheck: false,
			},
			{
				Type: "committee",
				ID:   "567",
				Data: map[string]any{
					"name":        "Security Committee",
					"description": "Handles security-related matters",
					"status":      "active",
					"tags":        []string{"active", "security"},
				},
				TransactionBodyStub: model.TransactionBodyStub{
					ObjectRef:            "committee:567",
					ObjectType:           "committee",
					ObjectID:             "567",
					Public:               false,
					AccessCheckObject:    "committee:567",
					AccessCheckRelation:  "member",
					HistoryCheckObject:   "committee:567",
					HistoryCheckRelation: "viewer",
				},
				NeedCheck: true,
			},
			{
				Type: "meeting",
				ID:   "101",
				Data: map[string]any{
					"name":        "Monthly Board Meeting",
					"description": "Regular board meeting for project governance",
					"status":      "active",
					"tags":        []string{"active", "governance"},
				},
				TransactionBodyStub: model.TransactionBodyStub{
					ObjectRef:            "meeting:101",
					ObjectType:           "meeting",
					ObjectID:             "101",
					Public:               false,
					AccessCheckObject:    "", // Empty to simulate missing access control info
					AccessCheckRelation:  "",
					HistoryCheckObject:   "meeting:101",
					HistoryCheckRelation: "viewer",
				},
				NeedCheck: true,
			},
			{
				Type: "project",
				ID:   "789",
				Data: map[string]any{
					"name":        "Internal Security Project",
					"slug":        "internal-security-project",
					"description": "Private security-focused project",
					"status":      "active",
					"tags":        []string{"active", "security", "private"},
				},
				TransactionBodyStub: model.TransactionBodyStub{
					ObjectRef:            "project:789",
					ObjectType:           "project",
					ObjectID:             "789",
					Public:               false,
					AccessCheckObject:    "project:789",
					AccessCheckRelation:  "contributor",
					HistoryCheckObject:   "project:789",
					HistoryCheckRelation: "viewer",
				},
				NeedCheck: true,
			},
		},
	}
}

// QueryResources implements the ResourceSearcher interface with mock data
func (m *MockResourceSearcher) QueryResources(ctx context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {
	slog.DebugContext(ctx, "executing mock search", "criteria", criteria)

	var filteredResources []model.Resource

	// Filter by type
	if criteria.ResourceType != nil {
		for _, resource := range m.resources {
			if resource.Type == *criteria.ResourceType {
				filteredResources = append(filteredResources, resource)
			}
		}
	} else {
		filteredResources = m.resources
	}

	// Filter by name (case-insensitive substring search)
	if criteria.Name != nil {
		var nameFilteredResources []model.Resource
		searchName := strings.ToLower(*criteria.Name)

		for _, resource := range filteredResources {
			if data, ok := resource.Data.(map[string]interface{}); ok {
				// Check name field
				nameMatch := false
				if name, ok := data["name"].(string); ok {
					if strings.Contains(strings.ToLower(name), searchName) {
						nameMatch = true
					}
				}

				// For projects, also check slug field
				if !nameMatch && resource.Type == "project" {
					if slug, ok := data["slug"].(string); ok {
						if strings.Contains(strings.ToLower(slug), searchName) {
							nameMatch = true
						}
					}
				}

				if nameMatch {
					nameFilteredResources = append(nameFilteredResources, resource)
				}
			}
		}
		filteredResources = nameFilteredResources
	}

	// Filter by object refs (pre-filter from FGA grants)
	if len(criteria.ObjectRefs) > 0 {
		refSet := make(map[string]struct{}, len(criteria.ObjectRefs))
		for _, ref := range criteria.ObjectRefs {
			refSet[ref] = struct{}{}
		}
		var refsFiltered []model.Resource
		for _, resource := range filteredResources {
			if _, ok := refSet[resource.ObjectRef]; ok {
				refsFiltered = append(refsFiltered, resource)
			}
		}
		filteredResources = refsFiltered
	}

	// Filter by tags (OR logic - any tag matches)
	if len(criteria.Tags) > 0 {
		var tagFilteredResources []model.Resource

		for _, resource := range filteredResources {
			if data, ok := resource.Data.(map[string]interface{}); ok {
				if resourceTags, ok := data["tags"].([]string); ok {
					// OR logic: resource must have any of the requested tags
					for _, requestedTag := range criteria.Tags {
						for _, resourceTag := range resourceTags {
							if requestedTag == resourceTag {
								tagFilteredResources = append(tagFilteredResources, resource)
								goto nextResourceOR
							}
						}
					}
				}
			}
		nextResourceOR:
		}
		filteredResources = tagFilteredResources
	}

	// Filter by tags_all (AND logic - all tags must match)
	if len(criteria.TagsAll) > 0 {
		var tagAllFilteredResources []model.Resource

		for _, resource := range filteredResources {
			if data, ok := resource.Data.(map[string]interface{}); ok {
				if resourceTags, ok := data["tags"].([]string); ok {
					// AND logic: resource must have all requested tags
					matchCount := 0
					for _, requestedTag := range criteria.TagsAll {
						for _, resourceTag := range resourceTags {
							if requestedTag == resourceTag {
								matchCount++
								break
							}
						}
					}
					if matchCount == len(criteria.TagsAll) {
						tagAllFilteredResources = append(tagAllFilteredResources, resource)
					}
				}
			}
		}
		filteredResources = tagAllFilteredResources
	}

	// Sort results (simplified implementation)
	m.sortResources(filteredResources, criteria.SortBy)

	result := &model.SearchResult{
		Resources: filteredResources,
	}

	slog.DebugContext(ctx, "mock search completed", "results_count", len(result.Resources))
	return result, nil
}

// filterForCount applies the count-route criteria (type, name, tags,
// tags_all, public/private) to the in-memory resources.
func (m *MockResourceSearcher) filterForCount(criteria model.SearchCriteria) []model.Resource {
	var filtered []model.Resource
	for _, resource := range m.resources {
		if criteria.PublicOnly && !resource.Public {
			continue
		}
		if criteria.PrivateOnly && resource.Public {
			continue
		}
		if criteria.ResourceType != nil && resource.Type != *criteria.ResourceType {
			continue
		}
		if criteria.Name != nil && !m.nameMatches(resource, *criteria.Name) {
			continue
		}
		tags := resourceTags(resource)
		if len(criteria.Tags) > 0 && !anyTag(tags, criteria.Tags) {
			continue
		}
		if len(criteria.TagsAll) > 0 && !allTags(tags, criteria.TagsAll) {
			continue
		}
		filtered = append(filtered, resource)
	}
	return filtered
}

func (m *MockResourceSearcher) nameMatches(resource model.Resource, search string) bool {
	data, ok := resource.Data.(map[string]any)
	if !ok {
		return false
	}
	search = strings.ToLower(search)
	if name, ok := data["name"].(string); ok && strings.Contains(strings.ToLower(name), search) {
		return true
	}
	if resource.Type == "project" {
		if slug, ok := data["slug"].(string); ok && strings.Contains(strings.ToLower(slug), search) {
			return true
		}
	}
	return false
}

// resourceTags reads the tags slice the mock keeps under Data["tags"].
func resourceTags(resource model.Resource) []string {
	data, ok := resource.Data.(map[string]any)
	if !ok {
		return nil
	}
	switch tags := data["tags"].(type) {
	case []string:
		return tags
	case []any:
		out := make([]string, 0, len(tags))
		for _, tag := range tags {
			if str, ok := tag.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func anyTag(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

func allTags(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// accessKey mirrors the indexed access_check_query field.
func accessKey(resource model.Resource) string {
	if resource.AccessCheckObject == "" || resource.AccessCheckRelation == "" {
		return ""
	}
	return resource.AccessCheckObject + "#" + resource.AccessCheckRelation
}

// CountPublic implements the ResourceSearcher interface with mock data
func (m *MockResourceSearcher) CountPublic(ctx context.Context, criteria model.SearchCriteria) (int, error) {
	slog.DebugContext(ctx, "executing mock public count", "criteria", criteria)
	if m.countPublicError != nil {
		return 0, m.countPublicError
	}
	if m.countPublicResponse != nil {
		return *m.countPublicResponse, nil
	}
	criteria.PublicOnly = true
	return len(m.filterForCount(criteria)), nil
}

// AccessBuckets implements the ResourceSearcher interface with mock data:
// private resources grouped by access key, sorted by key, paged like a
// composite aggregation.
func (m *MockResourceSearcher) AccessBuckets(ctx context.Context, criteria model.SearchCriteria, request model.AccessBucketRequest) (*model.AccessBucketPage, error) {
	slog.DebugContext(ctx, "executing mock access bucket page", "criteria", criteria, "request", request)
	if m.accessBucketsError != nil {
		return nil, m.accessBucketsError
	}
	m.accessBucketCalls++
	if len(m.accessBucketPages) > 0 {
		idx := m.accessBucketCalls - 1
		if idx >= len(m.accessBucketPages) {
			idx = len(m.accessBucketPages) - 1
		}
		return m.accessBucketPages[idx], nil
	}

	criteria.PrivateOnly = true
	counts := make(map[string]uint64)
	for _, resource := range m.filterForCount(criteria) {
		key := accessKey(resource)
		if key == "" {
			continue
		}
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		if request.After != nil && key <= *request.After {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = len(keys)
	}
	page := &model.AccessBucketPage{Buckets: []model.AggregationBucket{}}
	for i, key := range keys {
		if i >= pageSize {
			break
		}
		page.Buckets = append(page.Buckets, model.AggregationBucket{Key: key, DocCount: counts[key]})
	}
	if len(page.Buckets) > 0 {
		last := page.Buckets[len(page.Buckets)-1].Key
		page.AfterKey = &last
	}
	return page, nil
}

// AuthorizedAggregation implements the ResourceSearcher interface with mock
// data: groups and distinct tag values over public resources and private
// resources whose access key was granted.
func (m *MockResourceSearcher) AuthorizedAggregation(ctx context.Context, criteria model.SearchCriteria, aggregation model.CountAggregation) (*model.CountAggregationResult, error) {
	slog.DebugContext(ctx, "executing mock authorized aggregation", "criteria", criteria, "aggregation", aggregation)
	if m.authorizedAggregationError != nil {
		return nil, m.authorizedAggregationError
	}
	if m.authorizedAggregationResponse != nil {
		return m.authorizedAggregationResponse, nil
	}

	granted := make(map[string]struct{}, len(aggregation.AuthorizedKeys))
	for _, key := range aggregation.AuthorizedKeys {
		granted[key] = struct{}{}
	}
	criteria.PublicOnly = false
	criteria.PrivateOnly = false
	var authorized []model.Resource
	for _, resource := range m.filterForCount(criteria) {
		if resource.Public {
			if aggregation.IncludePublic {
				authorized = append(authorized, resource)
			}
			continue
		}
		if _, ok := granted[accessKey(resource)]; ok {
			authorized = append(authorized, resource)
		}
	}

	result := &model.CountAggregationResult{
		Groups:         []model.CountGroup{},
		GroupsComplete: true,
		MetricComplete: true,
	}

	if aggregation.GroupByPrefix != "" {
		prefix := aggregation.GroupByPrefix + ":"
		counts := make(map[string]uint64)
		for _, resource := range authorized {
			for _, tag := range resourceTags(resource) {
				if strings.HasPrefix(tag, prefix) {
					counts[strings.TrimPrefix(tag, prefix)]++
				}
			}
		}
		groups := make([]model.CountGroup, 0, len(counts))
		for key, count := range counts {
			groups = append(groups, model.CountGroup{Key: key, Count: count})
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			return groups[i].Key < groups[j].Key
		})
		if aggregation.GroupBySize > 0 && len(groups) > aggregation.GroupBySize {
			groups = groups[:aggregation.GroupBySize]
			result.GroupsComplete = false
		}
		result.Groups = groups
	}

	if aggregation.CardinalityPrefix != "" {
		prefix := aggregation.CardinalityPrefix + ":"
		distinct := make(map[string]struct{})
		for _, resource := range authorized {
			for _, tag := range resourceTags(resource) {
				if strings.HasPrefix(tag, prefix) {
					distinct[tag] = struct{}{}
				}
			}
		}
		result.MetricValue = uint64(len(distinct))
		if aggregation.MaxDistinct > 0 && len(distinct) >= aggregation.MaxDistinct {
			result.MetricValue = uint64(aggregation.MaxDistinct)
			result.MetricComplete = false
		}
	}

	return result, nil
}

// IsReady implements the ResourceSearcher interface (always ready for mock)
func (m *MockResourceSearcher) IsReady(ctx context.Context) error {
	if m.isReadyError != nil {
		return m.isReadyError
	}
	return nil
}

// sortResources sorts the resources based on the sort criteria
func (m *MockResourceSearcher) sortResources(resources []model.Resource, sort string) {
	// This is a simplified sorting implementation
	// In a real implementation, you'd use proper sorting algorithms

	if sort == "name_desc" {
		// Reverse the order for descending sort
		for i := len(resources)/2 - 1; i >= 0; i-- {
			opp := len(resources) - 1 - i
			resources[i], resources[opp] = resources[opp], resources[i]
		}
	}
}

// AddResource adds a resource to the mock data (useful for testing)
func (m *MockResourceSearcher) AddResource(resource model.Resource) {
	// Ensure the resource has proper access control fields if not already set
	if resource.ObjectRef == "" {
		resource.ObjectRef = resource.Type + ":" + resource.ID
	}
	if resource.ObjectType == "" {
		resource.ObjectType = resource.Type
	}
	if resource.ObjectID == "" {
		resource.ObjectID = resource.ID
	}

	// Set default access control values if not specified
	if resource.AccessCheckObject == "" && resource.AccessCheckRelation == "" {
		// Default to requiring access check with reasonable defaults
		resource.AccessCheckObject = resource.Type + ":" + resource.ID
		resource.AccessCheckRelation = "viewer"
		resource.NeedCheck = true
	}

	m.resources = append(m.resources, resource)
}

// NewResourceWithDefaults creates a new resource with proper default access control fields
func NewResourceWithDefaults(resourceType, id string, data map[string]any, isPublic bool) model.Resource {
	// For projects, ensure slug is included if not present
	if resourceType == "project" {
		if _, hasSlug := data["slug"]; !hasSlug {
			if name, hasName := data["name"].(string); hasName {
				// Generate slug from name: lowercase, replace spaces with hyphens
				slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
				data["slug"] = slug
			}
		}
	}

	resource := model.Resource{
		Type: resourceType,
		ID:   id,
		Data: data,
		TransactionBodyStub: model.TransactionBodyStub{
			ObjectRef:            resourceType + ":" + id,
			ObjectType:           resourceType,
			ObjectID:             id,
			Public:               isPublic,
			AccessCheckObject:    resourceType + ":" + id,
			AccessCheckRelation:  "viewer",
			HistoryCheckObject:   resourceType + ":" + id,
			HistoryCheckRelation: "viewer",
		},
		NeedCheck: !isPublic,
	}

	// If not public, set appropriate access check defaults
	if !isPublic {
		switch resourceType {
		case "committee":
			resource.AccessCheckRelation = "member"
		case "project":
			resource.AccessCheckRelation = "viewer"
		case "meeting":
			resource.AccessCheckRelation = "attendee"
		default:
			resource.AccessCheckRelation = "viewer"
		}
	}

	return resource
}

// ClearResources clears all resources (useful for testing)
func (m *MockResourceSearcher) ClearResources() {
	m.resources = []model.Resource{}
}

// GetResourceCount returns the total number of resources
func (m *MockResourceSearcher) GetResourceCount() int {
	return len(m.resources)
}

// Test helper methods for setting up mock responses

// SetCountPublicResponse forces the value returned by CountPublic.
func (m *MockResourceSearcher) SetCountPublicResponse(count int) {
	m.countPublicResponse = &count
}

// SetCountPublicError forces CountPublic to fail.
func (m *MockResourceSearcher) SetCountPublicError(err error) {
	m.countPublicError = err
}

// SetAccessBucketPages forces the pages returned by successive AccessBuckets
// calls, in order; the last page repeats once exhausted.
func (m *MockResourceSearcher) SetAccessBucketPages(pages ...*model.AccessBucketPage) {
	m.accessBucketPages = pages
	m.accessBucketCalls = 0
}

// AccessBucketCalls returns how many AccessBuckets pages were requested.
func (m *MockResourceSearcher) AccessBucketCalls() int {
	return m.accessBucketCalls
}

// SetAccessBucketsError forces AccessBuckets to fail.
func (m *MockResourceSearcher) SetAccessBucketsError(err error) {
	m.accessBucketsError = err
}

// SetAuthorizedAggregationResponse forces the value returned by AuthorizedAggregation.
func (m *MockResourceSearcher) SetAuthorizedAggregationResponse(response *model.CountAggregationResult) {
	m.authorizedAggregationResponse = response
}

// SetAuthorizedAggregationError forces AuthorizedAggregation to fail.
func (m *MockResourceSearcher) SetAuthorizedAggregationError(err error) {
	m.authorizedAggregationError = err
}

// SetIsReadyError sets the mock error for IsReady calls
func (m *MockResourceSearcher) SetIsReadyError(err error) {
	m.isReadyError = err
}
