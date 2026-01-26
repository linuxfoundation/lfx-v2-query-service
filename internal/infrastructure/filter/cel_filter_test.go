// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package filter

import (
	"context"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
)

func TestNewCELFilter(t *testing.T) {
	assertion := assert.New(t)

	filter, err := NewCELFilter()

	assertion.NoError(err)
	assertion.NotNil(filter)
	assertion.NotNil(filter.env)
	assertion.NotNil(filter.programCache)
}

func TestCELFilter_Filter_EmptyExpression(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project 1"}},
		{ID: "2", Type: "project", Data: map[string]any{"name": "Project 2"}},
	}

	result, err := filter.Filter(context.Background(), resources, "")

	assertion.NoError(err)
	assertion.Equal(2, len(result))
	assertion.Equal(resources, result)
}

func TestCELFilter_Filter_SimpleEquality(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"slug": "tlf", "status": "active"}},
		{ID: "2", Type: "project", Data: map[string]any{"slug": "linux", "status": "active"}},
		{ID: "3", Type: "project", Data: map[string]any{"slug": "tlf", "status": "inactive"}},
	}

	result, err := filter.Filter(context.Background(), resources, `data.slug == "tlf"`)

	assertion.NoError(err)
	assertion.Equal(2, len(result))
	assertion.Equal("1", result[0].ID)
	assertion.Equal("3", result[1].ID)
}

func TestCELFilter_Filter_MultipleConditions(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"status": "active", "priority": 10}},
		{ID: "2", Type: "project", Data: map[string]any{"status": "active", "priority": 3}},
		{ID: "3", Type: "project", Data: map[string]any{"status": "inactive", "priority": 10}},
	}

	result, err := filter.Filter(context.Background(), resources, `data.status == "active" && data.priority > 5`)

	assertion.NoError(err)
	assertion.Equal(1, len(result))
	assertion.Equal("1", result[0].ID)
}

func TestCELFilter_Filter_TypeVariable(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
		{ID: "2", Type: "committee", Data: map[string]any{"name": "Committee"}},
		{ID: "3", Type: "project", Data: map[string]any{"name": "Another Project"}},
	}

	result, err := filter.Filter(context.Background(), resources, `resource_type == "project"`)

	assertion.NoError(err)
	assertion.Equal(2, len(result))
	assertion.Equal("1", result[0].ID)
	assertion.Equal("3", result[1].ID)
}

func TestCELFilter_Filter_IDVariable(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "123", Type: "project", Data: map[string]any{"name": "Project"}},
		{ID: "456", Type: "project", Data: map[string]any{"name": "Another"}},
	}

	result, err := filter.Filter(context.Background(), resources, `id == "123"`)

	assertion.NoError(err)
	assertion.Equal(1, len(result))
	assertion.Equal("123", result[0].ID)
}

func TestCELFilter_Filter_StringOperations(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Linux Foundation"}},
		{ID: "2", Type: "project", Data: map[string]any{"name": "Apache Foundation"}},
		{ID: "3", Type: "project", Data: map[string]any{"name": "Linux Kernel"}},
	}

	result, err := filter.Filter(context.Background(), resources, `data.name.startsWith("Linux")`)

	assertion.NoError(err)
	assertion.Equal(2, len(result))
	assertion.Equal("1", result[0].ID)
	assertion.Equal("3", result[1].ID)
}

func TestCELFilter_Filter_NumericComparisons(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expectedID string
	}{
		{
			name:       "greater than",
			expression: `data.count > 50`,
			expectedID: "1",
		},
		{
			name:       "less than",
			expression: `data.count < 30`,
			expectedID: "2",
		},
		{
			name:       "greater than or equal",
			expression: `data.count >= 50`,
			expectedID: "1",
		},
		{
			name:       "less than or equal",
			expression: `data.count <= 25`,
			expectedID: "2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)
			filter, _ := NewCELFilter()

			resources := []model.Resource{
				{ID: "1", Type: "project", Data: map[string]any{"count": 100}},
				{ID: "2", Type: "project", Data: map[string]any{"count": 25}},
			}

			result, err := filter.Filter(context.Background(), resources, tc.expression)

			assertion.NoError(err)
			assertion.Equal(1, len(result))
			assertion.Equal(tc.expectedID, result[0].ID)
		})
	}
}

func TestCELFilter_Filter_NestedFields(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{
			"settings": map[string]any{"notifications": map[string]any{"enabled": true}},
		}},
		{ID: "2", Type: "project", Data: map[string]any{
			"settings": map[string]any{"notifications": map[string]any{"enabled": false}},
		}},
	}

	result, err := filter.Filter(context.Background(), resources, `data.settings.notifications.enabled == true`)

	assertion.NoError(err)
	assertion.Equal(1, len(result))
	assertion.Equal("1", result[0].ID)
}

func TestCELFilter_Filter_InvalidExpression(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
	}

	result, err := filter.Filter(context.Background(), resources, `invalid syntax (((`)

	assertion.Error(err)
	assertion.Nil(result)
	assertion.Contains(err.Error(), "invalid filter expression")
}

func TestCELFilter_Filter_NonBooleanExpression(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
	}

	// Expression returns string, not boolean
	result, err := filter.Filter(context.Background(), resources, `data.name`)

	assertion.Error(err)
	assertion.Nil(result)
	assertion.Contains(err.Error(), "must return boolean")
}

func TestCELFilter_Filter_ExpressionTooLong(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
	}

	// Create expression longer than MaxExpressionLength
	longExpression := ""
	for i := 0; i < MaxExpressionLength+100; i++ {
		longExpression += "x"
	}

	result, err := filter.Filter(context.Background(), resources, longExpression)

	assertion.Error(err)
	assertion.Nil(result)
	assertion.Contains(err.Error(), "exceeds maximum length")
}

func TestCELFilter_Filter_MissingField(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
		{ID: "2", Type: "project", Data: map[string]any{"slug": "project-2"}},
	}

	// Lenient mode: skip resources that error during evaluation
	result, err := filter.Filter(context.Background(), resources, `data.slug == "project-2"`)

	assertion.NoError(err)
	assertion.Equal(1, len(result))
	assertion.Equal("2", result[0].ID)
}

func TestCELFilter_Filter_ContextCancellation(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := filter.Filter(ctx, resources, `data.name == "Project"`)

	assertion.Error(err)
	assertion.Nil(result)
}

func TestCELFilter_ProgramCaching(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	resources := []model.Resource{
		{ID: "1", Type: "project", Data: map[string]any{"name": "Project"}},
	}

	expression := `data.name == "Project"`

	// First call - should compile
	result1, err1 := filter.Filter(context.Background(), resources, expression)
	assertion.NoError(err1)
	assertion.Equal(1, len(result1))

	// Second call - should use cached program
	result2, err2 := filter.Filter(context.Background(), resources, expression)
	assertion.NoError(err2)
	assertion.Equal(1, len(result2))

	// Verify cache contains the expression
	assertion.NotNil(filter.programCache.get(expression))
}

func TestCELFilter_CacheExpiration(t *testing.T) {
	assertion := assert.New(t)
	filter, _ := NewCELFilter()

	expression := `data.name == "Project"`

	// Compile and cache
	_, err := filter.getOrCompileProgram(expression)
	assertion.NoError(err)

	// Verify it's in cache
	filter.programCache.mu.RLock()
	cacheSize := len(filter.programCache.cache)
	filter.programCache.mu.RUnlock()
	assertion.Equal(1, cacheSize, "expected 1 cached entry")

	// Manually expire the cache entry
	filter.programCache.mu.Lock()
	entry := filter.programCache.cache[expression]
	entry.expiresAt = time.Now().Add(-1 * time.Hour)
	filter.programCache.mu.Unlock()

	// Should return nil for expired entry
	assertion.Nil(filter.programCache.get(expression))

	// Verify expired entry was removed from cache
	filter.programCache.mu.RLock()
	cacheSize = len(filter.programCache.cache)
	filter.programCache.mu.RUnlock()
	assertion.Equal(0, cacheSize, "expected expired entry to be removed from cache")
}

func TestCELFilter_ComplexExpressions(t *testing.T) {
	tests := []struct {
		name          string
		expression    string
		resources     []model.Resource
		expectedCount int
		expectedIDs   []string
	}{
		{
			name:       "OR condition",
			expression: `data.status == "active" || data.status == "pending"`,
			resources: []model.Resource{
				{ID: "1", Type: "project", Data: map[string]any{"status": "active"}},
				{ID: "2", Type: "project", Data: map[string]any{"status": "pending"}},
				{ID: "3", Type: "project", Data: map[string]any{"status": "inactive"}},
			},
			expectedCount: 2,
			expectedIDs:   []string{"1", "2"},
		},
		{
			name:       "IN operator",
			expression: `data.status in ["active", "pending"]`,
			resources: []model.Resource{
				{ID: "1", Type: "project", Data: map[string]any{"status": "active"}},
				{ID: "2", Type: "project", Data: map[string]any{"status": "inactive"}},
			},
			expectedCount: 1,
			expectedIDs:   []string{"1"},
		},
		{
			name:       "Combined string and numeric",
			expression: `data.name.contains("Linux") && data.members > 10`,
			resources: []model.Resource{
				{ID: "1", Type: "project", Data: map[string]any{"name": "Linux Foundation", "members": 20}},
				{ID: "2", Type: "project", Data: map[string]any{"name": "Linux Kernel", "members": 5}},
				{ID: "3", Type: "project", Data: map[string]any{"name": "Apache", "members": 15}},
			},
			expectedCount: 1,
			expectedIDs:   []string{"1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertion := assert.New(t)
			filter, _ := NewCELFilter()

			result, err := filter.Filter(context.Background(), tc.resources, tc.expression)

			assertion.NoError(err)
			assertion.Equal(tc.expectedCount, len(result))

			for i, expectedID := range tc.expectedIDs {
				assertion.Equal(expectedID, result[i].ID)
			}
		})
	}
}

func TestProgramCache_CleanupExpired(t *testing.T) {
	assertion := assert.New(t)

	cache := &programCache{
		cache:   make(map[string]*cacheEntry),
		maxSize: 10,
	}

	// Add entries with different expiration times
	filter, _ := NewCELFilter()
	prg1, _ := filter.env.Program(nil) // Dummy program

	cache.cache["expired1"] = &cacheEntry{
		program:   prg1,
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	cache.cache["expired2"] = &cacheEntry{
		program:   prg1,
		expiresAt: time.Now().Add(-30 * time.Minute),
	}
	cache.cache["valid"] = &cacheEntry{
		program:   prg1,
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	cache.mu.Lock()
	cache.cleanupExpiredLocked()
	cache.mu.Unlock()

	assertion.Equal(1, len(cache.cache))
	assertion.NotNil(cache.cache["valid"])
}
