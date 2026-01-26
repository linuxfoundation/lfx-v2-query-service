// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// ResourceFilter defines the interface for filtering resources based on expressions
type ResourceFilter interface {
	// Filter applies an expression filter to a list of resources
	// Returns only resources that match the filter expression
	// If expression is empty or nil, returns all resources unfiltered
	Filter(ctx context.Context, resources []model.Resource, expression string) ([]model.Resource, error)
}
