// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"strings"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
)

// parseDateFilter parses a date string in ISO 8601 datetime or date-only format
// and returns it normalized for OpenSearch range queries.
// Date-only format (YYYY-MM-DD) is converted to:
// - Start of day (00:00:00 UTC) for date_from
// - End of day (23:59:59 UTC) for date_to
func parseDateFilter(dateStr string, isEndDate bool) (string, error) {
	if dateStr == "" {
		return "", nil
	}

	// Try parsing as ISO 8601 datetime first (e.g., 2025-01-10T15:30:00Z)
	t, err := time.Parse(time.RFC3339, dateStr)
	if err == nil {
		// Already in datetime format, return as-is
		return t.Format(time.RFC3339), nil
	}

	// Try parsing as date-only (e.g., 2025-01-10)
	t, err = time.Parse("2006-01-02", dateStr)
	if err != nil {
		return "", fmt.Errorf("invalid date format '%s': must be ISO 8601 datetime (2006-01-02T15:04:05Z) or date-only (2006-01-02)", dateStr)
	}

	// Convert date-only to datetime
	if isEndDate {
		// For end dates, use end of day (23:59:59 UTC)
		// Note: Using 23:59:59 instead of 23:59:59.999 for simplicity and OpenSearch compatibility
		t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
	} else {
		// For start dates, use start of day (00:00:00 UTC)
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}

	return t.Format(time.RFC3339), nil
}

// parseFilters parses filter strings in "field:value" format
// All fields are automatically prefixed with "data." to filter only within the data object
func parseFilters(filters []string) ([]model.FieldFilter, error) {
	if len(filters) == 0 {
		return nil, nil
	}

	parsed := make([]model.FieldFilter, 0, len(filters))
	for _, filter := range filters {
		parts := strings.SplitN(filter, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid filter format '%s': expected 'field:value'", filter)
		}
		fieldName := strings.TrimSpace(parts[0])
		if fieldName == "" {
			return nil, fmt.Errorf("invalid filter format '%s': field name cannot be empty", filter)
		}
		// Automatically prefix with "data." to ensure filtering only on data fields
		parsed = append(parsed, model.FieldFilter{
			Field: "data." + fieldName,
			Value: strings.TrimSpace(parts[1]),
		})
	}
	return parsed, nil
}

// payloadToCriteria converts the generated payload to domain search criteria
func (s *querySvcsrvc) payloadToCriteria(ctx context.Context, p *querysvc.QueryResourcesPayload) (model.SearchCriteria, error) {
	// Parse filters from "field:value" format
	filters, err := parseFilters(p.Filters)
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse filters", "error", err)
		return model.SearchCriteria{}, wrapError(ctx, err)
	}

	criteria := model.SearchCriteria{
		Name:         p.Name,
		Parent:       p.Parent,
		ResourceType: p.Type,
		Tags:         p.Tags,
		TagsAll:      p.TagsAll,
		Filters:      filters,
		CelFilter:    p.CelFilter,
		SortBy:       p.Sort,
		PageToken:    p.PageToken,
		PageSize:     p.PageSize,
	}
	switch p.Sort {
	case "name_asc":
		criteria.SortBy = "sort_name"
		criteria.SortOrder = "asc"
	case "name_desc":
		criteria.SortBy = "sort_name"
		criteria.SortOrder = "desc"
	case "updated_asc":
		criteria.SortBy = "updated_at"
		criteria.SortOrder = "asc"
	case "updated_desc":
		criteria.SortBy = "updated_at"
		criteria.SortOrder = "desc"
	}

	if criteria.PageToken != nil {
		pageToken, errPageToken := paging.DecodePageToken(ctx, *criteria.PageToken, global.PageTokenSecret(ctx))
		if errPageToken != nil {
			slog.ErrorContext(ctx, "failed to decode page token", "error", errPageToken)
			return criteria, wrapError(ctx, errPageToken)
		}
		criteria.SearchAfter = &pageToken
		slog.DebugContext(ctx, "decoded page token",
			"page_token", *criteria.PageToken,
			"decoded", pageToken,
		)
	}

	// Validate date filtering parameters
	if (p.DateFrom != nil || p.DateTo != nil) && p.DateField == nil {
		err := fmt.Errorf("date_field is required when using date_from or date_to")
		slog.ErrorContext(ctx, "invalid date filter parameters", "error", err)
		return criteria, wrapError(ctx, err)
	}

	// Handle date filtering parameters
	if p.DateField != nil {
		// Auto-prefix with "data." to scope to data object
		prefixedField := "data." + *p.DateField
		criteria.DateField = &prefixedField

		// Parse and normalize date_from
		if p.DateFrom != nil {
			normalizedFrom, err := parseDateFilter(*p.DateFrom, false)
			if err != nil {
				slog.ErrorContext(ctx, "invalid date_from format", "error", err, "date_from", *p.DateFrom)
				return criteria, wrapError(ctx, err)
			}
			criteria.DateFrom = &normalizedFrom
		}

		// Parse and normalize date_to
		if p.DateTo != nil {
			normalizedTo, err := parseDateFilter(*p.DateTo, true)
			if err != nil {
				slog.ErrorContext(ctx, "invalid date_to format", "error", err, "date_to", *p.DateTo)
				return criteria, wrapError(ctx, err)
			}
			criteria.DateTo = &normalizedTo
		}
	}

	return criteria, nil
}

// domainResultToResponse converts domain search result to generated response
func (s *querySvcsrvc) domainResultToResponse(result *model.SearchResult) *querysvc.QueryResourcesResult {
	response := &querysvc.QueryResourcesResult{
		Resources:    make([]*querysvc.Resource, len(result.Resources)),
		PageToken:    result.PageToken,
		CacheControl: result.CacheControl,
	}

	for i, domainResource := range result.Resources {
		// Create local copies to avoid taking addresses of loop variables
		resourceType := domainResource.Type
		resourceID := domainResource.ID
		response.Resources[i] = &querysvc.Resource{
			Type: &resourceType,
			ID:   &resourceID,
			Data: domainResource.Data,
		}
	}

	return response
}

func (s *querySvcsrvc) payloadToCountPublicCriteria(payload *querysvc.QueryResourcesCountPayload) (model.SearchCriteria, error) {
	// Parameters used for /<index>/_count search.
	criteria := model.SearchCriteria{
		GroupBySize: constants.DefaultBucketSize,
		// Page size is not passed to this endpoint.
		PageSize: -1,
		// For _count, we only want public resources.
		PublicOnly: true,
	}

	// Parse filters from "field:value" format
	filters, err := parseFilters(payload.Filters)
	if err != nil {
		return criteria, fmt.Errorf("invalid filters: %w", err)
	}

	// Set the criteria from the payload
	criteria.Tags = payload.Tags
	criteria.TagsAll = payload.TagsAll
	criteria.Filters = filters
	if payload.Name != nil {
		criteria.Name = payload.Name
	}
	if payload.Type != nil {
		criteria.ResourceType = payload.Type
	}
	if payload.Parent != nil {
		criteria.Parent = payload.Parent
	}

	// Validate date filtering parameters
	if (payload.DateFrom != nil || payload.DateTo != nil) && payload.DateField == nil {
		return criteria, fmt.Errorf("date_field is required when using date_from or date_to")
	}

	// Handle date filtering parameters
	if payload.DateField != nil {
		// Auto-prefix with "data." to scope to data object
		prefixedField := "data." + *payload.DateField
		criteria.DateField = &prefixedField

		// Parse and normalize date_from
		if payload.DateFrom != nil {
			normalizedFrom, err := parseDateFilter(*payload.DateFrom, false)
			if err != nil {
				return criteria, fmt.Errorf("invalid date_from: %w", err)
			}
			criteria.DateFrom = &normalizedFrom
		}

		// Parse and normalize date_to
		if payload.DateTo != nil {
			normalizedTo, err := parseDateFilter(*payload.DateTo, true)
			if err != nil {
				return criteria, fmt.Errorf("invalid date_to: %w", err)
			}
			criteria.DateTo = &normalizedTo
		}
	}

	return criteria, nil
}

func (s *querySvcsrvc) payloadToCountAggregationCriteria(payload *querysvc.QueryResourcesCountPayload) (model.SearchCriteria, error) {
	// Parameters used for the "group by" aggregated /<index>/_search search.
	criteria := model.SearchCriteria{
		GroupBySize: constants.DefaultBucketSize,
		// We only want the aggregation, not the actual results.
		PageSize: 0,
		// The aggregation results will only count private resources.
		PrivateOnly: true,
		// Set the attribute to aggregate on.
		// Use .keyword subfield for aggregation on text fields
		GroupBy: "access_check_query.keyword",
	}

	// Parse filters from "field:value" format
	filters, err := parseFilters(payload.Filters)
	if err != nil {
		return criteria, fmt.Errorf("invalid filters: %w", err)
	}

	// Set the criteria from the payload
	criteria.Tags = payload.Tags
	criteria.TagsAll = payload.TagsAll
	criteria.Filters = filters
	if payload.Name != nil {
		criteria.Name = payload.Name
	}
	if payload.Type != nil {
		criteria.ResourceType = payload.Type
	}
	if payload.Parent != nil {
		criteria.Parent = payload.Parent
	}

	// Validate date filtering parameters
	if (payload.DateFrom != nil || payload.DateTo != nil) && payload.DateField == nil {
		return criteria, fmt.Errorf("date_field is required when using date_from or date_to")
	}

	// Handle date filtering parameters
	if payload.DateField != nil {
		// Auto-prefix with "data." to scope to data object
		prefixedField := "data." + *payload.DateField
		criteria.DateField = &prefixedField

		// Parse and normalize date_from
		if payload.DateFrom != nil {
			normalizedFrom, err := parseDateFilter(*payload.DateFrom, false)
			if err != nil {
				return criteria, fmt.Errorf("invalid date_from: %w", err)
			}
			criteria.DateFrom = &normalizedFrom
		}

		// Parse and normalize date_to
		if payload.DateTo != nil {
			normalizedTo, err := parseDateFilter(*payload.DateTo, true)
			if err != nil {
				return criteria, fmt.Errorf("invalid date_to: %w", err)
			}
			criteria.DateTo = &normalizedTo
		}
	}

	return criteria, nil
}

func (s *querySvcsrvc) domainCountResultToResponse(result *model.CountResult) *querysvc.QueryResourcesCountResult {
	return &querysvc.QueryResourcesCountResult{
		Count:        uint64(result.Count),
		HasMore:      result.HasMore,
		CacheControl: result.CacheControl,
	}
}

// payloadToOrganizationCriteria converts the generated payload to domain organization search criteria
func (s *querySvcsrvc) payloadToOrganizationCriteria(ctx context.Context, p *querysvc.QueryOrgsPayload) model.OrganizationSearchCriteria {
	criteria := model.OrganizationSearchCriteria{
		Name:   p.Name,
		Domain: p.Domain,
	}
	return criteria
}

// domainOrganizationToResponse converts domain organization result to generated response
func (s *querySvcsrvc) domainOrganizationToResponse(org *model.Organization) *querysvc.Organization {
	return &querysvc.Organization{
		Name:      &org.Name,
		Domain:    &org.Domain,
		Industry:  &org.Industry,
		Sector:    &org.Sector,
		Employees: &org.Employees,
	}
}

// payloadToOrganizationSuggestionCriteria converts the generated payload to domain organization suggestion criteria
func (s *querySvcsrvc) payloadToOrganizationSuggestionCriteria(ctx context.Context, p *querysvc.SuggestOrgsPayload) model.OrganizationSuggestionCriteria {
	criteria := model.OrganizationSuggestionCriteria{
		Query: p.Query,
	}
	return criteria
}

// domainOrganizationSuggestionsToResponse converts domain organization suggestions result to generated response
func (s *querySvcsrvc) domainOrganizationSuggestionsToResponse(result *model.OrganizationSuggestionsResult) *querysvc.SuggestOrgsResult {
	if result == nil || len(result.Suggestions) == 0 {
		return &querysvc.SuggestOrgsResult{Suggestions: []*querysvc.OrganizationSuggestion{}}
	}
	suggestions := make([]*querysvc.OrganizationSuggestion, len(result.Suggestions))

	for i, domainSuggestion := range result.Suggestions {
		suggestions[i] = &querysvc.OrganizationSuggestion{
			Name:   domainSuggestion.Name,
			Domain: domainSuggestion.Domain,
			Logo:   domainSuggestion.Logo,
		}
	}

	return &querysvc.SuggestOrgsResult{
		Suggestions: suggestions,
	}
}
