// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log/slog"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
)

// payloadToCriteria converts the generated payload to domain search criteria
func (s *querySvcsrvc) payloadToCriteria(ctx context.Context, p *querysvc.QueryResourcesPayload) (model.SearchCriteria, error) {

	criteria := model.SearchCriteria{
		Name:         p.Name,
		Parent:       p.Parent,
		ResourceType: p.Type,
		Tags:         p.Tags,
		TagsAll:      p.TagsAll,
		CelFilter:    p.CelFilter,
		SortBy:       p.Sort,
		PageToken:    p.PageToken,
		PageSize:     constants.DefaultPageSize,
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

func (s *querySvcsrvc) payloadToCountPublicCriteria(payload *querysvc.QueryResourcesCountPayload) model.SearchCriteria {
	// Parameters used for /<index>/_count search.
	criteria := model.SearchCriteria{
		GroupBySize: constants.DefaultBucketSize,
		// Page size is not passed to this endpoint.
		PageSize: -1,
		// For _count, we only want public resources.
		PublicOnly: true,
	}

	// Set the criteria from the payload
	criteria.Tags = payload.Tags
	criteria.TagsAll = payload.TagsAll
	if payload.Name != nil {
		criteria.Name = payload.Name
	}
	if payload.Type != nil {
		criteria.ResourceType = payload.Type
	}
	if payload.Parent != nil {
		criteria.ParentRef = payload.Parent
	}

	return criteria
}

func (s *querySvcsrvc) payloadToCountAggregationCriteria(payload *querysvc.QueryResourcesCountPayload) model.SearchCriteria {
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

	// Set the criteria from the payload
	criteria.Tags = payload.Tags
	criteria.TagsAll = payload.TagsAll
	if payload.Name != nil {
		criteria.Name = payload.Name
	}
	if payload.Type != nil {
		criteria.ResourceType = payload.Type
	}
	if payload.Parent != nil {
		criteria.ParentRef = payload.Parent
	}

	return criteria
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
