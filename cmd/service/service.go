// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log/slog"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/log"

	"goa.design/goa/v3/security"
)

// query-svc service implementation using clean architecture.
type querySvcsrvc struct {
	resourceService     service.ResourceSearcher
	organizationService service.OrganizationSearcher
	auth                port.Authenticator
}

// JWTAuth implements the authorization logic for service "query-svc" for the
// "jwt" security scheme.
func (s *querySvcsrvc) JWTAuth(ctx context.Context, token string, scheme *security.JWTScheme) (context.Context, error) {

	// Parse the Heimdall-authorized principal from the token.
	principal, err := s.auth.ParsePrincipal(ctx, token, slog.Default())
	if err != nil {
		return ctx, wrapError(ctx, err)
	}

	// Log the principal for debugging purposes in all logs for this request.
	ctx = log.AppendCtx(ctx, slog.String(string(constants.PrincipalAttribute), principal))

	// Return a new context containing the principal as a value.
	return context.WithValue(ctx, constants.PrincipalContextID, principal), nil
}

// Locate resources by their type or parent, or use typeahead search to query
// resources by a display name or similar alias.
func (s *querySvcsrvc) QueryResources(ctx context.Context, p *querysvc.QueryResourcesPayload) (res *querysvc.QueryResourcesResult, err error) {

	slog.DebugContext(ctx, "querySvc.query-resources",
		"name", p.Name,
	)

	// Convert payload to domain criteria
	criteria, errCriteria := s.payloadToCriteria(ctx, p)
	if errCriteria != nil {
		slog.ErrorContext(ctx, "failed to convert payload to criteria", "error", errCriteria)
		return nil, wrapError(ctx, errCriteria)
	}

	// Execute search using the service layer
	result, errQueryResources := s.resourceService.QueryResources(ctx, criteria)
	if errQueryResources != nil {
		return nil, wrapError(ctx, errQueryResources)
	}

	// Convert domain result to response
	res = s.domainResultToResponse(result)
	return res, nil
}

// QueryResourcesCount returns an aggregate count of resources the user hase
// access to, by implementing an aggregation over the stored OpenFGA
// relationship.
func (s *querySvcsrvc) QueryResourcesCount(ctx context.Context, p *querysvc.QueryResourcesCountPayload) (*querysvc.QueryResourcesCountResult, error) {

	slog.DebugContext(ctx, "querySvc.query-resource-counts",
		"name", p.Name,
	)

	// Convert payload to domain criteria
	countCriteria, errCountCriteria := s.payloadToCountPublicCriteria(p)
	if errCountCriteria != nil {
		return nil, wrapError(ctx, errCountCriteria)
	}
	aggregationCriteria, errAggCriteria := s.payloadToCountAggregationCriteria(p)
	if errAggCriteria != nil {
		return nil, wrapError(ctx, errAggCriteria)
	}

	// Execute search using the service layer
	result, errQueryResources := s.resourceService.QueryResourcesCount(ctx, countCriteria, aggregationCriteria)
	if errQueryResources != nil {
		return nil, wrapError(ctx, errQueryResources)
	}

	return s.domainCountResultToResponse(result), nil
}

// Locate a single organization by name or domain.
func (s *querySvcsrvc) QueryOrgs(ctx context.Context, p *querysvc.QueryOrgsPayload) (res *querysvc.Organization, err error) {

	slog.DebugContext(ctx, "querySvc.query-orgs",
		"name", p.Name,
		"domain", p.Domain,
	)

	// Convert payload to domain criteria
	criteria := s.payloadToOrganizationCriteria(ctx, p)

	// Execute search using the service layer
	result, errQueryOrgs := s.organizationService.QueryOrganizations(ctx, criteria)
	if errQueryOrgs != nil {
		return nil, wrapError(ctx, errQueryOrgs)
	}

	// Convert domain result to response
	res = s.domainOrganizationToResponse(result)
	return res, nil
}

// Get organization suggestions for typeahead search based on a query.
func (s *querySvcsrvc) SuggestOrgs(ctx context.Context, p *querysvc.SuggestOrgsPayload) (res *querysvc.SuggestOrgsResult, err error) {

	slog.DebugContext(ctx, "querySvc.suggest-orgs",
		"query", p.Query,
	)

	// Convert payload to domain criteria
	criteria := s.payloadToOrganizationSuggestionCriteria(ctx, p)

	// Execute search using the service layer
	result, errSuggestOrgs := s.organizationService.SuggestOrganizations(ctx, criteria)
	if errSuggestOrgs != nil {
		return nil, wrapError(ctx, errSuggestOrgs)
	}

	// Convert domain result to response
	res = s.domainOrganizationSuggestionsToResponse(result)
	return res, nil
}

// Check if the service is able to take inbound requests.
func (s *querySvcsrvc) Readyz(ctx context.Context) (res []byte, err error) {
	errIsReady := s.resourceService.IsReady(ctx)
	if errIsReady != nil {
		slog.ErrorContext(ctx, "querySvc.readyz failed", "error", errIsReady)
		return nil, wrapError(ctx, errIsReady)
	}

	return []byte("OK\n"), nil
}

// Check if the service is alive.
func (s *querySvcsrvc) Livez(ctx context.Context) (res []byte, err error) {
	// This always returns as long as the service is still running. As this
	// endpoint is expected to be used as a Kubernetes liveness check, this
	// service must likewise self-detect non-recoverable errors and
	// self-terminate.
	return []byte("OK\n"), nil
}

// NewQuerySvc returns the query-svc service implementation.
func NewQuerySvc(resourceSearcher port.ResourceSearcher,
	accessControlChecker port.AccessControlChecker,
	resourceFilter port.ResourceFilter,
	organizationSearcher port.OrganizationSearcher,
	auth port.Authenticator,
) querysvc.Service {
	resourceService := service.NewResourceSearch(resourceSearcher, accessControlChecker, resourceFilter)
	organizationService := service.NewOrganizationSearch(organizationSearcher)
	return &querySvcsrvc{
		resourceService:     resourceService,
		organizationService: organizationService,
		auth:                auth,
	}
}
