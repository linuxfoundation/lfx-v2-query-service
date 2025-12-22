// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

// ResourceSearcher defines the interface for resource search operations
// This abstraction allows different search implementations (OpenSearch, etc.)
// without the domain layer knowing about specific implementations
type ResourceSearcher interface {
	// QueryResources searches for resources based on the provided criteria
	QueryResources(ctx context.Context, criteria model.SearchCriteria) (*model.SearchResult, error)

	// QueryResourcesCount searches for resources based on the provided criteria
	QueryResourcesCount(ctx context.Context, countCriteria model.SearchCriteria, aggregationCriteria model.SearchCriteria) (*model.CountResult, error)

	// IsReady checks if the search service is ready
	IsReady(ctx context.Context) error
}

// ResourceSearch handles resource-related business operations
// It depends on abstractions (interfaces) rather than concrete implementations
type ResourceSearch struct {
	resourceSearcher port.ResourceSearcher
	accessChecker    port.AccessControlChecker
	resourceFilter   port.ResourceFilter
}

// QueryResources performs resource search with business logic validation
func (s *ResourceSearch) QueryResources(ctx context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {

	slog.DebugContext(ctx, "starting resource search",
		"name", criteria.Name,
		"type", criteria.ResourceType,
		"parent", criteria.Parent,
	)

	// It seems that Goa v3 does not natively support complex conditional validations
	// like “at least one of these fields must be set"
	if err := s.validateSearchCriteria(criteria); err != nil {
		slog.ErrorContext(ctx, "search criteria validation failed", "error", err)
		return nil, errors.NewValidation(
			"search criteria validation failed",
			err,
		)
	}

	// Grab the principal which was stored into the context by the security handler.
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		// This should not happen; the Auther always sets this or errors.
		return nil, errors.NewValidation("missing principal in context")
	}
	if principal == constants.AnonymousPrincipal {
		// For an anonymous use, we will use the "public:true" OpenSearch term
		// filter, instead of OpenFGA, to filter results for performance.
		slog.DebugContext(ctx, "anonymous user detected, applying public-only filter")
		criteria.PublicOnly = true
	}

	// Log the search operation
	slog.DebugContext(ctx, "validated search criteria, proceeding with search")

	// Delegate to the search implementation
	result, err := s.resourceSearcher.QueryResources(ctx, criteria)
	if err != nil {
		slog.ErrorContext(ctx, "search operation failed while executing query resources",
			"error", err,
		)
		return nil, fmt.Errorf("search operation failed: %w", err)
	}

	// Apply CEL filter if provided (before access control to reduce the number of resources to check).
	// Note: applying this filter before access control and pagination can change the effective result set
	// seen by the caller, which may cause pagination tokens (based on the unfiltered set) to skip or miss
	// resources when the CEL expression significantly reduces the results.
	if criteria.CelFilter != nil && *criteria.CelFilter != "" {
		slog.DebugContext(ctx, "applying CEL filter",
			"expression", *criteria.CelFilter,
			"resource_count_before", len(result.Resources),
		)
		filteredResources, errFilter := s.resourceFilter.Filter(ctx, result.Resources, *criteria.CelFilter)
		if errFilter != nil {
			slog.ErrorContext(ctx, "CEL filter failed",
				"error", errFilter,
				"expression", *criteria.CelFilter,
			)
			return nil, fmt.Errorf("filter expression failed: %w", errFilter)
		}
		result.Resources = filteredResources
		slog.DebugContext(ctx, "CEL filter applied",
			"resource_count_after", len(result.Resources),
		)
	}

	slog.DebugContext(ctx, "checking access control for resources",
		"resource_count", len(result.Resources),
	)

	messageCheckAccess := s.BuildMessage(ctx, principal, result)

	searchResult := &model.SearchResult{
		PageToken: result.PageToken,
	}

	// Check access control for the resources if needed
	checkedResources, errCheckAccess := s.CheckAccess(ctx, principal, result.Resources, messageCheckAccess)
	if errCheckAccess != nil {
		slog.ErrorContext(ctx, "access control check failed",
			"error", errCheckAccess,
			"message", string(messageCheckAccess),
		)
		return nil, fmt.Errorf("access control check failed: %w", errCheckAccess)
	}
	searchResult.Resources = checkedResources

	slog.DebugContext(ctx, "resource search completed",
		"query_count", len(result.Resources),
		"response_after_access_check", len(searchResult.Resources),
	)

	if principal == constants.AnonymousPrincipal {
		// Set a cache control header for anonymous users.
		cacheControl := constants.AnonymousCacheControlHeader
		searchResult.CacheControl = &cacheControl
	}

	return searchResult, nil
}

// validateSearchCriteria validates the search criteria according to business rules
func (s *ResourceSearch) validateSearchCriteria(criteria model.SearchCriteria) error {
	// At least one search parameter must be provided
	if criteria.Name == nil && criteria.Parent == nil && criteria.ResourceType == nil && len(criteria.Tags) == 0 {
		return fmt.Errorf("at least one search parameter must be provided: name, parent, type, or tags")
	}

	return nil
}

func (s *ResourceSearch) BuildMessage(ctx context.Context, principal string, result *model.SearchResult) []byte {

	// avoid duplicate resource references in the result
	seenRefs := make(map[string]struct{}, len(result.Resources))

	// estimate the size of each line in the access check message
	accessCheckMessage := make([]byte, 0, 80*len(result.Resources))
	for idx := range result.Resources {

		if _, seen := seenRefs[result.Resources[idx].ObjectRef]; seen {
			// Skip this result.
			continue
		}
		seenRefs[result.Resources[idx].ObjectRef] = struct{}{}

		if result.Resources[idx].Public {
			result.Resources[idx].NeedCheck = false
			continue
		}

		if result.Resources[idx].AccessCheckObject == "" || result.Resources[idx].AccessCheckRelation == "" {
			// Unable to perform access check without these fields.
			slog.WarnContext(ctx, "resource missing access control information, skipping",
				"object_ref", result.Resources[idx].ObjectRef,
				"object_type", result.Resources[idx].ObjectType,
				"object_id", result.Resources[idx].ObjectID,
			)
			result.Resources[idx].NeedCheck = true
			continue
		}
		result.Resources[idx].NeedCheck = true
		// make the access check message
		accessCheckMessage = append(accessCheckMessage, result.Resources[idx].AccessCheckObject...)
		accessCheckMessage = append(accessCheckMessage, byte('#'))
		accessCheckMessage = append(accessCheckMessage, result.Resources[idx].AccessCheckRelation...)
		accessCheckMessage = append(accessCheckMessage, []byte("@user:")...)
		accessCheckMessage = append(accessCheckMessage, []byte(principal)...)
		accessCheckMessage = append(accessCheckMessage, '\n')

	}
	return accessCheckMessage
}

func (s *ResourceSearch) CheckAccess(ctx context.Context, principal string, resourceList []model.Resource, accessCheckMessage []byte) ([]model.Resource, error) {

	var accessCheckResponses map[string]string
	if len(accessCheckMessage) > 0 {

		slog.DebugContext(ctx, "performing access control checks",
			"message", string(accessCheckMessage),
		)

		// Trim trailing newline.
		accessCheckMessage = accessCheckMessage[:len(accessCheckMessage)-1]
		accessCheckResult, errCheckAccess := s.accessChecker.CheckAccess(ctx, constants.AccessCheckSubject, accessCheckMessage, 15*time.Second)
		if errCheckAccess != nil {
			slog.ErrorContext(ctx, "access control check failed",
				"error", errCheckAccess,
				"message", string(accessCheckMessage),
			)
			return nil, fmt.Errorf("access control check failed: %w", errCheckAccess)
		}
		accessCheckResponses = accessCheckResult
	}

	var resources []model.Resource
	// ensuring the original order of resources
	for _, resource := range resourceList {
		addToList := false
		if resource.NeedCheck && resource.AccessCheckObject != "" && resource.AccessCheckRelation != "" {
			relationKey := resource.AccessCheckObject + "#" + resource.AccessCheckRelation + "@user:" + principal
			if allowed, ok := accessCheckResponses[relationKey]; ok && allowed == "true" {
				addToList = true
			}
		}
		if !resource.NeedCheck || addToList {
			resources = append(resources, resource)
		}
	}

	return resources, nil

}

func (s *ResourceSearch) QueryResourcesCount(
	ctx context.Context,
	publicCountCriteria model.SearchCriteria,
	aggregationCriteria model.SearchCriteria,
) (*model.CountResult, error) {

	slog.DebugContext(ctx, "starting resource count search",
		"count_criteria", publicCountCriteria,
		"aggregation_criteria", aggregationCriteria,
	)

	// Grab the principal which was stored into the context by the security handler.
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		// This should not happen; the Auther always sets this or errors.
		return nil, errors.NewValidation("missing principal in context")
	}

	// Log the search operation
	slog.DebugContext(ctx, "validated search criteria, proceeding with count search")

	// Delegate to the search implementation
	publicOnly := principal == constants.AnonymousPrincipal
	result, err := s.resourceSearcher.QueryResourcesCount(ctx, publicCountCriteria, aggregationCriteria, publicOnly)
	if err != nil {
		slog.ErrorContext(ctx, "search operation failed while executing query resources",
			"error", err,
		)
		return nil, fmt.Errorf("search operation failed: %w", err)
	}

	// If the principal is anonymous, we can return the result immediately without checking access control
	// since we already retrieved the public-only count.
	if principal == constants.AnonymousPrincipal {
		slog.DebugContext(ctx, "returning anonymous count result",
			"count", result.Count,
		)
		// Set a cache control header for anonymous users.
		cacheControl := constants.AnonymousCacheControlHeader
		result.CacheControl = &cacheControl
		return result, nil
	}

	slog.DebugContext(ctx, "checking access control for private resources",
		"aggregations", result.Aggregation,
	)

	messageCheckAccess := s.BuildCountMessage(ctx, principal, result, aggregationCriteria)

	// Check access control for the resources to determine the authorized response count
	privateCount, err := s.CheckCountAccess(ctx, principal, result, messageCheckAccess)
	if err != nil {
		slog.ErrorContext(ctx, "access control check failed",
			"error", err,
		)
		return nil, fmt.Errorf("access control check failed: %w", err)
	}
	// The count already contains the count of public resources, so we need to add the count of private resources.
	result.Count += int(privateCount)

	// Check for bucket overflow.
	// There could be more buckets than the page size, and therefore more results.
	if result.Aggregation.SumOtherDocCount > 0 {
		result.HasMore = true
	}

	return result, nil
}

func (s *ResourceSearch) BuildCountMessage(ctx context.Context, principal string, result *model.CountResult, aggregationCriteria model.SearchCriteria) []byte {

	// Create a map to store the "doc_count" of each aggregation bucket.
	docCountMap := make(map[string]uint64, aggregationCriteria.PageSize)

	// estimate the size of each line in the access check message
	accessCheckMessage := make([]byte, 0, 80*aggregationCriteria.PageSize)

	for _, bucket := range result.Aggregation.Buckets {
		docCountMap[bucket.Key] = bucket.DocCount
		accessCheckMessage = append(accessCheckMessage, bucket.Key...)
		accessCheckMessage = append(accessCheckMessage, []byte("@user:")...)
		accessCheckMessage = append(accessCheckMessage, []byte(principal)...)
		accessCheckMessage = append(accessCheckMessage, '\n')
	}

	return accessCheckMessage
}

func (s *ResourceSearch) CheckCountAccess(ctx context.Context, principal string, result *model.CountResult, accessCheckMessage []byte) (uint64, error) {
	var accessCheckResponses map[string]string
	if len(accessCheckMessage) > 0 {
		slog.DebugContext(ctx, "performing access control checks",
			"message", string(accessCheckMessage),
		)

		// Trim trailing newline.
		accessCheckMessage = accessCheckMessage[:len(accessCheckMessage)-1]
		accessCheckResult, errCheckAccess := s.accessChecker.CheckAccess(ctx, constants.AccessCheckSubject, accessCheckMessage, 15*time.Second)
		if errCheckAccess != nil {
			slog.ErrorContext(ctx, "access control check failed",
				"error", errCheckAccess,
				"message", string(accessCheckMessage),
			)
			return 0, fmt.Errorf("access control check failed: %w", errCheckAccess)
		}
		accessCheckResponses = accessCheckResult
	}
	slog.DebugContext(ctx, "access check responses", "responses", accessCheckResponses)

	var count uint64
	for _, bucket := range result.Aggregation.Buckets {
		// The bucket.Key already contains the full access check query including the principal
		// e.g.: "committee:830513f8-0e77-4a48-a8e4-ede4c1a61f98#viewer@user:project_super_admin"
		// The BuildCountMessage function appends "@user:" + principal to create the access check key
		// So we need to use the same format here
		accessCheckKey := bucket.Key + "@user:" + principal
		slog.DebugContext(ctx, "checking access control for bucket",
			"bucket", bucket.Key,
			"access_check_key", accessCheckKey,
		)
		if allowed, ok := accessCheckResponses[accessCheckKey]; ok && allowed == "true" {
			count += bucket.DocCount
		}
	}

	return count, nil
}

func (s *ResourceSearch) IsReady(ctx context.Context) error {
	if err := s.resourceSearcher.IsReady(ctx); err != nil {
		return err
	}

	if err := s.accessChecker.IsReady(ctx); err != nil {
		return err
	}

	return nil
}

// NewResourceSearch creates a new ResourceSearch instance
func NewResourceSearch(resourceSearcher port.ResourceSearcher, accessChecker port.AccessControlChecker, resourceFilter port.ResourceFilter) ResourceSearcher {
	return &ResourceSearch{
		resourceSearcher: resourceSearcher,
		accessChecker:    accessChecker,
		resourceFilter:   resourceFilter,
	}
}
