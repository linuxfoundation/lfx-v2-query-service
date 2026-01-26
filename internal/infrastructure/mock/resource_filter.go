// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// MockResourceFilter is a mock implementation of ResourceFilter for testing
type MockResourceFilter struct{}

// NewMockResourceFilter creates a new MockResourceFilter
func NewMockResourceFilter() *MockResourceFilter {
	return &MockResourceFilter{}
}

// Filter returns all resources unfiltered (no-op filter for testing)
func (m *MockResourceFilter) Filter(ctx context.Context, resources []model.Resource, expression string) ([]model.Resource, error) {
	// Mock filter just returns all resources unchanged
	return resources, nil
}
