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

	// QueryResourcesCount counts the resources matching the criteria that the
	// caller may see, optionally grouped or reduced to a metric.
	QueryResourcesCount(ctx context.Context, publicCriteria model.SearchCriteria, privateCriteria model.SearchCriteria, aggregation model.CountAggregation) (*model.CountResult, error)

	// IsReady checks if the search service is ready
	IsReady(ctx context.Context) error
}

// Config carries the tunables of the resource search service. Zero values are
// replaced by the defaults in pkg/constants; the constructor validates the
// result.
type Config struct {
	// AccessCheckTimeout bounds each batched access check sent to fga-sync.
	AccessCheckTimeout time.Duration
	// ReadTuplesTimeout bounds the direct tuple read used by filter_grants=direct.
	ReadTuplesTimeout time.Duration
	// AccessBucketPage is the number of access-key buckets fetched and
	// checked per page of the count walk (1..constants.MaxAccessBucketPage).
	AccessBucketPage int
	// MaxAccessBuckets caps the number of access-key buckets a single count
	// walks before it stops and reports has_more (>= AccessBucketPage).
	MaxAccessBuckets int
}

// DefaultConfig returns the configuration used when nothing is set in the
// environment.
func DefaultConfig() Config {
	return Config{
		AccessCheckTimeout: 15 * time.Second,
		ReadTuplesTimeout:  15 * time.Second,
		AccessBucketPage:   constants.DefaultAccessBucketPage,
		MaxAccessBuckets:   constants.DefaultMaxAccessBuckets,
	}
}

// withDefaults fills zero values from DefaultConfig.
func (c Config) withDefaults() Config {
	defaults := DefaultConfig()
	if c.AccessCheckTimeout == 0 {
		c.AccessCheckTimeout = defaults.AccessCheckTimeout
	}
	if c.ReadTuplesTimeout == 0 {
		c.ReadTuplesTimeout = defaults.ReadTuplesTimeout
	}
	if c.AccessBucketPage == 0 {
		c.AccessBucketPage = defaults.AccessBucketPage
	}
	if c.MaxAccessBuckets == 0 {
		c.MaxAccessBuckets = defaults.MaxAccessBuckets
	}
	return c
}

// Validate reports the first invalid setting.
func (c Config) Validate() error {
	if c.AccessCheckTimeout <= 0 {
		return fmt.Errorf("access check timeout must be positive, got %s", c.AccessCheckTimeout)
	}
	if c.ReadTuplesTimeout <= 0 {
		return fmt.Errorf("read tuples timeout must be positive, got %s", c.ReadTuplesTimeout)
	}
	if c.AccessBucketPage < 1 || c.AccessBucketPage > constants.MaxAccessBucketPage {
		return fmt.Errorf("access bucket page must be between 1 and %d, got %d", constants.MaxAccessBucketPage, c.AccessBucketPage)
	}
	if c.MaxAccessBuckets < c.AccessBucketPage {
		return fmt.Errorf("max access buckets (%d) must be at least the access bucket page (%d)", c.MaxAccessBuckets, c.AccessBucketPage)
	}
	return nil
}

// ResourceSearch handles resource-related business operations
// It depends on abstractions (interfaces) rather than concrete implementations
type ResourceSearch struct {
	resourceSearcher port.ResourceSearcher
	accessChecker    port.AccessControlChecker
	resourceFilter   port.ResourceFilter
	config           Config
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

	// If filter_grants=direct is requested, resolve the user's direct FGA grants and
	// use them as a pre-query terms filter on object_ref in OpenSearch.
	if criteria.FilterGrants != nil && *criteria.FilterGrants == "direct" {
		if principal == constants.AnonymousPrincipal {
			return nil, errors.NewValidation("filter_grants requires authentication")
		}
		objectRefs, errTuples := s.accessChecker.ReadTuples(ctx, "user:"+principal, *criteria.ResourceType, s.config.ReadTuplesTimeout)
		if errTuples != nil {
			slog.ErrorContext(ctx, "failed to read FGA tuples for filter_grants",
				"error", errTuples,
				"user", principal,
				"object_type", *criteria.ResourceType,
			)
			return nil, fmt.Errorf("failed to read FGA tuples: %w", errTuples)
		}
		if len(objectRefs) == 0 {
			slog.DebugContext(ctx, "no FGA grants found, returning empty result",
				"user", principal,
				"object_type", *criteria.ResourceType,
			)
			return &model.SearchResult{Resources: []model.Resource{}}, nil
		}
		criteria.ObjectRefs = objectRefs
		slog.DebugContext(ctx, "applied FGA grant filter",
			"user", principal,
			"object_type", *criteria.ResourceType,
			"grant_count", len(objectRefs),
		)
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
	if criteria.Name == nil && criteria.Parent == nil && criteria.ResourceType == nil && len(criteria.Tags) == 0 && criteria.FilterGrants == nil {
		return fmt.Errorf("at least one search parameter must be provided: name, parent, type, tags, or filter_grants")
	}

	// filter_grants requires type so we can look up grants by object type
	if criteria.FilterGrants != nil && criteria.ResourceType == nil {
		return fmt.Errorf("type is required when filter_grants is set")
	}

	return nil
}

func (s *ResourceSearch) BuildMessage(ctx context.Context, principal string, result *model.SearchResult) []byte {

	// Many resources (e.g. meeting registrants, past-meeting participants) share the
	// same parent AccessCheckObject/AccessCheckRelation by design, so only the emitted
	// check line is deduped here. Classification below (Public / missing fields /
	// NeedCheck) always runs for every resource: CheckAccess independently re-derives
	// each resource's relation key and looks it up, so skipping classification on a
	// "duplicate" would leave NeedCheck at its zero value (false) and cause that
	// resource to be returned unchecked.
	seenKeys := make(map[string]struct{}, len(result.Resources))

	// estimate the size of each line in the access check message
	accessCheckMessage := make([]byte, 0, 80*len(result.Resources))
	for idx := range result.Resources {

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

		relationKey := result.Resources[idx].AccessCheckObject + "#" + result.Resources[idx].AccessCheckRelation
		if _, seen := seenKeys[relationKey]; seen {
			// Already emitted a check line for this object#relation pair.
			continue
		}
		seenKeys[relationKey] = struct{}{}

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
		accessCheckResult, errCheckAccess := s.accessChecker.CheckAccess(ctx, constants.AccessCheckSubject, accessCheckMessage, s.config.AccessCheckTimeout)
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

// QueryResourcesCount counts the matching resources the caller may see.
//
// For an anonymous principal the answer is the public /_count alone. For an
// authenticated principal the service additionally walks the private
// resources grouped by access-check key in pages of config.AccessBucketPage
// buckets, access-checks each page in one batched request, and adds the
// document counts of the granted keys. The walk stops at a short page
// (exhaustive) or once config.MaxAccessBuckets buckets have been walked
// (HasMore = true). When aggregation asks for groups or a metric, they are
// computed afterwards over the authorized set only.
func (s *ResourceSearch) QueryResourcesCount(
	ctx context.Context,
	publicCriteria model.SearchCriteria,
	privateCriteria model.SearchCriteria,
	aggregation model.CountAggregation,
) (*model.CountResult, error) {

	slog.DebugContext(ctx, "starting resource count search",
		"public_criteria", publicCriteria,
		"private_criteria", privateCriteria,
		"aggregation", aggregation,
	)

	// Grab the principal which was stored into the context by the security handler.
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		// This should not happen; the Auther always sets this or errors.
		return nil, errors.NewValidation("missing principal in context")
	}

	publicCount, err := s.resourceSearcher.CountPublic(ctx, publicCriteria)
	if err != nil {
		slog.ErrorContext(ctx, "search operation failed while counting public resources",
			"error", err,
		)
		return nil, fmt.Errorf("search operation failed: %w", err)
	}

	result := &model.CountResult{
		Count: publicCount,
	}

	// Anonymous callers see public resources only; no access checks needed.
	aggregation.IncludePublic = true
	if principal == constants.AnonymousPrincipal {
		slog.DebugContext(ctx, "returning anonymous count result",
			"count", result.Count,
		)
		cacheControl := constants.AnonymousCacheControlHeader
		result.CacheControl = &cacheControl
	} else {
		authorizedKeys, privateCount, hasMore, errWalk := s.walkAccessBuckets(ctx, principal, privateCriteria)
		if errWalk != nil {
			return nil, errWalk
		}
		result.Count += int(privateCount)
		result.HasMore = hasMore
		aggregation.AuthorizedKeys = authorizedKeys
	}

	if !aggregation.HasWork() {
		return result, nil
	}

	// The authorized set is public documents plus private documents carrying
	// one of the granted keys. len(AuthorizedKeys) <= MaxAccessBuckets (5000
	// by default), well below OpenSearch's index.max_terms_count default of
	// 65536, so the terms clause is always accepted.
	if aggregation.PageSize == 0 {
		aggregation.PageSize = s.config.AccessBucketPage
	}
	if aggregation.MaxDistinct == 0 {
		aggregation.MaxDistinct = s.config.MaxAccessBuckets
	}
	aggregationResult, err := s.resourceSearcher.AuthorizedAggregation(ctx, publicCriteriaWithoutPublicOnly(publicCriteria), aggregation)
	if err != nil {
		slog.ErrorContext(ctx, "search operation failed while aggregating authorized resources",
			"error", err,
		)
		return nil, fmt.Errorf("search operation failed: %w", err)
	}
	if aggregation.GroupByPrefix != "" {
		result.Groups = aggregationResult.Groups
		groupsComplete := aggregationResult.GroupsComplete
		result.GroupsComplete = &groupsComplete
	}
	if aggregation.CardinalityPrefix != "" {
		metricValue := aggregationResult.MetricValue
		metricComplete := aggregationResult.MetricComplete
		result.MetricValue = &metricValue
		result.MetricComplete = &metricComplete
	}

	return result, nil
}

// publicCriteriaWithoutPublicOnly returns the caller's criteria with the
// public/private switches cleared, for queries whose visibility is expressed
// by an explicit authorized-set filter instead.
func publicCriteriaWithoutPublicOnly(criteria model.SearchCriteria) model.SearchCriteria {
	criteria.PublicOnly = false
	criteria.PrivateOnly = false
	return criteria
}

// walkAccessBuckets pages the private resources grouped by access-check key,
// access-checks each page, and returns the granted keys, the number of
// private documents they cover, and whether the walk stopped at the cap.
//
// Stopping rule: a page with fewer buckets than the page size is the last one
// (an after_key on it is ignored). After a full page, if the buckets walked so
// far reach the cap, the walk stops without requesting the next page and
// reports hasMore; pages are never split.
func (s *ResourceSearch) walkAccessBuckets(ctx context.Context, principal string, privateCriteria model.SearchCriteria) ([]string, uint64, bool, error) {
	started := time.Now()
	pageSize := s.config.AccessBucketPage
	maxBuckets := s.config.MaxAccessBuckets

	var (
		authorizedKeys []string
		privateCount   uint64
		walked         int
		after          *string
		hasMore        bool
		pages          int
	)

	for {
		pages++
		page, err := s.resourceSearcher.AccessBuckets(ctx, privateCriteria, model.AccessBucketRequest{
			PageSize: pageSize,
			After:    after,
		})
		if err != nil {
			slog.ErrorContext(ctx, "search operation failed while walking access buckets",
				"error", err,
				"page", pages,
			)
			return nil, 0, false, fmt.Errorf("search operation failed: %w", err)
		}
		slog.DebugContext(ctx, "access bucket page fetched",
			"page", pages,
			"buckets", len(page.Buckets),
		)

		if len(page.Buckets) > 0 {
			message := s.BuildCountMessage(ctx, principal, page.Buckets)
			pageCount, granted, errCheck := s.CheckCountAccess(ctx, principal, page.Buckets, message)
			if errCheck != nil {
				return nil, 0, false, errCheck
			}
			privateCount += pageCount
			authorizedKeys = append(authorizedKeys, granted...)
		}
		walked += len(page.Buckets)

		if len(page.Buckets) < pageSize {
			break
		}
		if walked >= maxBuckets {
			hasMore = true
			break
		}
		if page.AfterKey == nil {
			// Defensive: a full page without a cursor cannot be continued.
			break
		}
		after = page.AfterKey
	}

	elapsed := time.Since(started)
	if pages > 5 {
		slog.InfoContext(ctx, "access bucket walk exceeded five pages",
			"pages", pages,
			"buckets", walked,
			"authorized_keys", len(authorizedKeys),
			"has_more", hasMore,
			"elapsed", elapsed,
		)
	} else {
		slog.DebugContext(ctx, "access bucket walk completed",
			"pages", pages,
			"buckets", walked,
			"authorized_keys", len(authorizedKeys),
			"has_more", hasMore,
			"elapsed", elapsed,
		)
	}

	return authorizedKeys, privateCount, hasMore, nil
}

// BuildCountMessage renders one access-check line per bucket in the format
// fga-sync expects: "{access_check_query}@user:{principal}\n".
func (s *ResourceSearch) BuildCountMessage(ctx context.Context, principal string, buckets []model.AggregationBucket) []byte {

	// estimate the size of each line in the access check message
	accessCheckMessage := make([]byte, 0, 80*len(buckets))

	for _, bucket := range buckets {
		accessCheckMessage = append(accessCheckMessage, bucket.Key...)
		accessCheckMessage = append(accessCheckMessage, []byte("@user:")...)
		accessCheckMessage = append(accessCheckMessage, []byte(principal)...)
		accessCheckMessage = append(accessCheckMessage, '\n')
	}

	return accessCheckMessage
}

// CheckCountAccess sends one batched access check for the buckets and returns
// the summed document count of the granted buckets plus their keys.
//
// A failed check is a ServiceUnavailable error: the count must never be
// returned as if complete when part of the authorized set is unknown.
func (s *ResourceSearch) CheckCountAccess(ctx context.Context, principal string, buckets []model.AggregationBucket, accessCheckMessage []byte) (uint64, []string, error) {
	var accessCheckResponses map[string]string
	if len(accessCheckMessage) > 0 {
		slog.DebugContext(ctx, "performing access control checks",
			"message", string(accessCheckMessage),
		)

		// Trim trailing newline.
		accessCheckMessage = accessCheckMessage[:len(accessCheckMessage)-1]
		accessCheckResult, errCheckAccess := s.accessChecker.CheckAccess(ctx, constants.AccessCheckSubject, accessCheckMessage, s.config.AccessCheckTimeout)
		if errCheckAccess != nil {
			slog.ErrorContext(ctx, "access control check failed",
				"error", errCheckAccess,
				"message", string(accessCheckMessage),
			)
			return 0, nil, errors.NewServiceUnavailable("access control check failed", errCheckAccess)
		}
		accessCheckResponses = accessCheckResult
	}
	slog.DebugContext(ctx, "access check responses", "responses", accessCheckResponses)

	var count uint64
	granted := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		// The bucket.Key is the stored access_check_query
		// ("{access_check_object}#{access_check_relation}"); BuildCountMessage
		// appended "@user:" + principal, so the response is keyed the same way.
		accessCheckKey := bucket.Key + "@user:" + principal
		if allowed, ok := accessCheckResponses[accessCheckKey]; ok && allowed == "true" {
			count += bucket.DocCount
			granted = append(granted, bucket.Key)
		}
	}

	return count, granted, nil
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

// NewResourceSearch creates a new ResourceSearch instance. Zero values in
// config are replaced by DefaultConfig; an invalid config is an error.
func NewResourceSearch(resourceSearcher port.ResourceSearcher, accessChecker port.AccessControlChecker, resourceFilter port.ResourceFilter, config Config) (ResourceSearcher, error) {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid resource search config: %w", err)
	}
	return &ResourceSearch{
		resourceSearcher: resourceSearcher,
		accessChecker:    accessChecker,
		resourceFilter:   resourceFilter,
		config:           config,
	}, nil
}
