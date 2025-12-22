// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

// FieldFilter represents a single field:value filter pair
type FieldFilter struct {
	Field string
	Value string
}

// SearchCriteria encapsulates all possible search parameters
type SearchCriteria struct {
	// Tags to filter resources with OR logic (any tag matches)
	Tags []string
	// TagsAll to filter resources with AND logic (all tags must match)
	TagsAll []string
	// Filters for direct field term clauses (AND logic - all must match)
	Filters []FieldFilter
	// Resource name or alias; supports typeahead
	Name *string
	// Parent (for navigation; varies by object type)
	Parent *string
	// ParentRef is a reference to the parent resource
	ParentRef *string
	// ResourceType to search
	ResourceType *string
	// SearchAfter is used for pagination
	SearchAfter *string
	// Sortby order for results
	SortBy string
	// SortOrder for results
	SortOrder string
	// Opaque token for pagination
	PageToken *string
	// Pagesize for pagination
	PageSize int
	// PublicOnly indicates if only public resources should be returned
	PublicOnly bool
	// PrivateOnly indicates if only private resources should be returned
	PrivateOnly bool
	// GroupBy indicates the field to group by
	GroupBy string
	// GroupBySize indicates the size of the group by
	GroupBySize int
}

// SearchResult contains the results of a resource search
type SearchResult struct {
	// Resources found
	Resources []Resource
	// Opaque token if more results are available
	PageToken *string
	// Cache control header
	CacheControl *string
	// Total number of resources found
	Total int
}

// CountResult contains the results of a resource count search
type CountResult struct {
	// Count number of resources found
	Count int
	// Aggregations
	Aggregation TermsAggregation
	// HasMore indicates if there are more results
	HasMore bool
	// Cache control header
	CacheControl *string
}

// OrganizationSearchCriteria encapsulates search parameters for organizations
type OrganizationSearchCriteria struct {
	// Organization name
	Name *string
	// Organization domain or website URL
	Domain *string
}

// OrganizationSuggestionCriteria encapsulates search parameters for organization suggestions
type OrganizationSuggestionCriteria struct {
	// Search query for organization suggestions
	Query string
}
