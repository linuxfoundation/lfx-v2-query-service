// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
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

// commonQueryParams holds the query fields shared by QueryResourcesPayload and
// QueryResourcesCountPayload. Populate it from either payload type and pass it
// to applyCommonFields to avoid repeating filter-parsing and date-handling logic
// in every converter.
type commonQueryParams struct {
	Name       *string
	Parent     *string
	Type       *string
	Tags       []string
	TagsAll    []string
	Filters    []string
	FiltersAll []string
	FiltersOr  []string
	DateField  *string
	DateFrom   *string
	DateTo     *string
}

// applyCommonFields populates the shared filter and date fields on criteria from p.
// It returns an error for invalid filter formats or date parameter combinations.
// It does not log; callers are responsible for logging and wrapping the error.
func applyCommonFields(criteria *model.SearchCriteria, p commonQueryParams) error {
	filters, filtersAll, filtersOr, err := parseFilterSets(p.Filters, p.FiltersAll, p.FiltersOr)
	if err != nil {
		return err
	}
	criteria.Name = p.Name
	criteria.Parent = p.Parent
	criteria.ResourceType = p.Type
	criteria.Tags = p.Tags
	criteria.TagsAll = p.TagsAll
	criteria.Filters = filters
	criteria.FiltersAll = filtersAll
	criteria.FiltersOr = filtersOr

	if (p.DateFrom != nil || p.DateTo != nil) && p.DateField == nil {
		return fmt.Errorf("date_field is required when using date_from or date_to")
	}

	if p.DateField != nil {
		prefixedField := "data." + *p.DateField
		criteria.DateField = &prefixedField

		if p.DateFrom != nil {
			normalizedFrom, err := parseDateFilter(*p.DateFrom, false)
			if err != nil {
				return fmt.Errorf("invalid date_from: %w", err)
			}
			criteria.DateFrom = &normalizedFrom
		}

		if p.DateTo != nil {
			normalizedTo, err := parseDateFilter(*p.DateTo, true)
			if err != nil {
				return fmt.Errorf("invalid date_to: %w", err)
			}
			criteria.DateTo = &normalizedTo
		}
	}

	return nil
}

// parseFilterSets parses the three filter params (filters, filters_all, filters_or) in one call.
// Returns an error that already includes the param name for context.
func parseFilterSets(filters, filtersAll, filtersOr []string) ([]model.FieldFilter, []model.FieldFilter, []model.FieldFilter, error) {
	f, err := parseFilters(filters)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid filters: %w", err)
	}
	fa, err := parseFilters(filtersAll)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid filters_all: %w", err)
	}
	fo, err := parseFilters(filtersOr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid filters_or: %w", err)
	}
	return f, fa, fo, nil
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
	criteria := model.SearchCriteria{
		CelFilter:    p.CelFilter,
		FilterGrants: p.FilterGrants,
		SortBy:       p.Sort,
		PageToken:    p.PageToken,
		PageSize:     p.PageSize,
	}

	params := commonQueryParams{
		Name: p.Name, Parent: p.Parent, Type: p.Type,
		Tags: p.Tags, TagsAll: p.TagsAll,
		Filters: p.Filters, FiltersAll: p.FiltersAll, FiltersOr: p.FiltersOr,
		DateField: p.DateField, DateFrom: p.DateFrom, DateTo: p.DateTo,
	}
	if err := applyCommonFields(&criteria, params); err != nil {
		slog.ErrorContext(ctx, "invalid query parameters", "error", err)
		return model.SearchCriteria{}, wrapError(ctx, err)
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
	case "best_match":
		criteria.SortBy = "_score"
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

// countCommonParams extracts the shared search parameters of a count payload.
func countCommonParams(payload *querysvc.QueryResourcesCountPayload) commonQueryParams {
	return commonQueryParams{
		Name: payload.Name, Parent: payload.Parent, Type: payload.Type,
		Tags: payload.Tags, TagsAll: payload.TagsAll,
		Filters: payload.Filters, FiltersAll: payload.FiltersAll, FiltersOr: payload.FiltersOr,
		DateField: payload.DateField, DateFrom: payload.DateFrom, DateTo: payload.DateTo,
	}
}

// payloadToCountPublicCriteria builds the criteria of the public /_count.
func (s *querySvcsrvc) payloadToCountPublicCriteria(payload *querysvc.QueryResourcesCountPayload) (model.SearchCriteria, error) {
	criteria := model.SearchCriteria{
		PageSize:   -1,   // page size is not used for _count
		PublicOnly: true, // _count only counts public resources
	}
	err := applyCommonFields(&criteria, countCommonParams(payload))
	return criteria, err
}

// payloadToCountPrivateCriteria builds the criteria of the access-key walk
// over private resources. The field aggregated on is resolved by the searcher
// from the live index mapping, not chosen here.
func (s *querySvcsrvc) payloadToCountPrivateCriteria(payload *querysvc.QueryResourcesCountPayload) (model.SearchCriteria, error) {
	criteria := model.SearchCriteria{
		PageSize:    0,    // aggregation only; no result hits needed
		PrivateOnly: true, // the walk covers private resources only
	}
	err := applyCommonFields(&criteria, countCommonParams(payload))
	return criteria, err
}

// tagPrefixPattern is the shape of a group_by prefix and of the prefix inside
// a cardinality metric. It matches the Goa pattern on group_by.
var tagPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const (
	// metricCardinalityPrefix is the only metric family the count route
	// supports: "cardinality:<tag_prefix>".
	metricCardinalityPrefix = "cardinality:"
	// errMetricShape is returned for any metric that is not
	// cardinality:<tag_prefix>. Sums are named explicitly because they are
	// the obvious next ask and impossible on this index: data is a
	// flat_object, whose subfields cannot be aggregated.
	errMetricShape = "metric must be cardinality:<tag_prefix>; sum is not available on this index (data fields are flat_object)"
	// errMetricPerGroup is returned when group_by and metric are combined.
	errMetricPerGroup = "metric per group is not supported; group first, then count each group with tags"
)

// payloadToCountAggregation validates group_by, group_by_size and metric and
// builds the aggregation the count route computes over authorized resources.
func (s *querySvcsrvc) payloadToCountAggregation(payload *querysvc.QueryResourcesCountPayload) (model.CountAggregation, error) {
	aggregation := model.CountAggregation{
		GroupBySize: constants.DefaultGroupBySize,
	}

	if payload.GroupBy != nil && payload.Metric != nil {
		return aggregation, errors.NewValidation(errMetricPerGroup)
	}

	if payload.GroupBy != nil {
		if !tagPrefixPattern.MatchString(*payload.GroupBy) {
			return aggregation, errors.NewValidation("group_by must match ^[a-z][a-z0-9_]*$")
		}
		aggregation.GroupByPrefix = *payload.GroupBy
	}
	if payload.GroupBySize != nil {
		if *payload.GroupBySize < 1 || *payload.GroupBySize > constants.MaxGroupBySize {
			return aggregation, errors.NewValidation(fmt.Sprintf("group_by_size must be between 1 and %d", constants.MaxGroupBySize))
		}
		aggregation.GroupBySize = *payload.GroupBySize
	}

	if payload.Metric != nil {
		metric := *payload.Metric
		if !strings.HasPrefix(metric, metricCardinalityPrefix) {
			return aggregation, errors.NewValidation(errMetricShape)
		}
		prefix := strings.TrimPrefix(metric, metricCardinalityPrefix)
		if !tagPrefixPattern.MatchString(prefix) {
			return aggregation, errors.NewValidation(errMetricShape)
		}
		aggregation.CardinalityPrefix = prefix
	}

	return aggregation, nil
}

// domainCountResultToResponse converts the domain count result; the group and
// metric attributes are present only when they were requested.
func (s *querySvcsrvc) domainCountResultToResponse(result *model.CountResult) *querysvc.QueryResourcesCountResult {
	response := &querysvc.QueryResourcesCountResult{
		Count:          uint64(result.Count),
		HasMore:        result.HasMore,
		GroupsComplete: result.GroupsComplete,
		MetricValue:    result.MetricValue,
		MetricComplete: result.MetricComplete,
		CacheControl:   result.CacheControl,
	}
	if result.GroupsComplete != nil {
		response.Groups = make([]*querysvc.CountGroup, 0, len(result.Groups))
		for _, group := range result.Groups {
			response.Groups = append(response.Groups, &querysvc.CountGroup{
				Key:   group.Key,
				Count: group.Count,
			})
		}
	}
	return response
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
