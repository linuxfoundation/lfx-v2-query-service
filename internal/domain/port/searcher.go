// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// ResourceSearcher defines the behavior for resource search operations
// This abstraction allows different search implementations (OpenSearch, etc.)
// without the domain layer knowing about specific implementations
type ResourceSearcher interface {
	// QueryResources searches for resources based on the provided criteria
	QueryResources(ctx context.Context, criteria model.SearchCriteria) (*model.SearchResult, error)

	// CountPublic counts the public resources matching the criteria.
	// The criteria must carry PublicOnly = true.
	CountPublic(ctx context.Context, criteria model.SearchCriteria) (int, error)

	// AccessBuckets returns one page of the private resources matching the
	// criteria, grouped by access-check key. The criteria must carry
	// PrivateOnly = true. Callers page with request.After until a page is
	// short (fewer buckets than request.PageSize).
	AccessBuckets(ctx context.Context, criteria model.SearchCriteria, request model.AccessBucketRequest) (*model.AccessBucketPage, error)

	// AuthorizedAggregation computes the grouped count and/or the distinct
	// tag-value metric described by aggregation over the resources matching
	// the criteria that the caller may see: public ones when
	// aggregation.IncludePublic is set, plus private ones whose access-check
	// key is in aggregation.AuthorizedKeys.
	AuthorizedAggregation(ctx context.Context, criteria model.SearchCriteria, aggregation model.CountAggregation) (*model.CountAggregationResult, error)

	// IsReady checks if the search service is ready
	IsReady(ctx context.Context) error
}

// OrganizationSearcher defines the behavior for organization search operations
// This abstraction allows different search implementations (External API, etc.)
// without the domain layer knowing about specific implementations
type OrganizationSearcher interface {
	// QueryOrganizations searches for organizations based on the provided criteria
	QueryOrganizations(ctx context.Context, criteria model.OrganizationSearchCriteria) (*model.Organization, error)

	// SuggestOrganizations returns organization suggestions for typeahead search
	SuggestOrganizations(ctx context.Context, criteria model.OrganizationSuggestionCriteria) (*model.OrganizationSuggestionsResult, error)

	// IsReady checks if the search service is ready
	IsReady(ctx context.Context) error
}
