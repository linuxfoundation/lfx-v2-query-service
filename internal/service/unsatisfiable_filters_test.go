// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsatisfiableFiltersRoutes(t *testing.T) {
	anyDoc := model.CarrierProbe{Kind: model.AnyDocument}
	date := model.CarrierProbe{Kind: model.DataField, Name: "start_time"}
	parent := model.CarrierProbe{Kind: model.ParentKind, Name: "project"}
	category := model.CarrierProbe{Kind: model.TagPrefix, Name: "category"}
	missingTag := model.CarrierProbe{Kind: model.TagPrefix, Name: "missing"}
	status := model.CarrierProbe{Kind: model.DataField, Name: "status"}
	missingField := model.CarrierProbe{Kind: model.DataField, Name: "missing"}
	since := stringPtr("2025-01-01")
	for _, tc := range []struct {
		name                string
		criteria            model.SearchCriteria
		carried             []model.CarrierProbe
		wantProbes          []model.CarrierProbe
		emptyType, disabled bool
		probeError          error
		message             string
	}{
		{name: "no type", criteria: model.SearchCriteria{Name: stringPtr("empty")}},
		{name: "empty type string", criteria: model.SearchCriteria{ResourceType: stringPtr("")}},
		{name: "unindexed type", emptyType: true, criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateFrom: stringPtr("2025-01-01")}, wantProbes: []model.CarrierProbe{anyDoc}},
		{name: "no dimensions"},
		{name: "date field without range is not a filter", criteria: model.SearchCriteria{DateField: stringPtr("data.start_time")}},
		{name: "date absent with upper bound only", criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateTo: since}, wantProbes: []model.CarrierProbe{anyDoc, date}, message: `date_field "start_time" is not carried by any indexed committee document`},
		{name: "date absent", criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateFrom: stringPtr("2025-01-01")}, wantProbes: []model.CarrierProbe{anyDoc, date}, message: `date_field "start_time" is not carried by any indexed committee document`},
		{name: "bare date field", criteria: model.SearchCriteria{DateField: stringPtr("start_time"), DateFrom: stringPtr("2025-01-01")}, wantProbes: []model.CarrierProbe{anyDoc, date}, carried: []model.CarrierProbe{date}},
		{name: "parent absent", criteria: model.SearchCriteria{Parent: stringPtr("project:private-value")}, wantProbes: []model.CarrierProbe{anyDoc, parent}, message: `parent kind "project:" is not carried by any indexed committee document`},
		{name: "parent without colon", criteria: model.SearchCriteria{Parent: stringPtr("project")}, wantProbes: []model.CarrierProbe{anyDoc, parent}, carried: []model.CarrierProbe{parent}},
		{name: "empty parent ignored", criteria: model.SearchCriteria{Parent: stringPtr("")}},
		{name: "AND tag absent", criteria: model.SearchCriteria{TagsAll: []string{"missing:private-value"}}, wantProbes: []model.CarrierProbe{anyDoc, missingTag}, message: `tag prefix "missing:" is not carried by any indexed committee document`},
		{name: "AND tags require every prefix", criteria: model.SearchCriteria{TagsAll: []string{"category:one", "missing:two"}}, carried: []model.CarrierProbe{category}, wantProbes: []model.CarrierProbe{anyDoc, category, missingTag}, message: `tag prefix "missing:" is not carried by any indexed committee document`},
		{name: "OR tags one carried", criteria: model.SearchCriteria{Tags: []string{"missing:one", "category:two"}}, carried: []model.CarrierProbe{category}, wantProbes: []model.CarrierProbe{anyDoc, missingTag, category}},
		{name: "OR tags none carried", criteria: model.SearchCriteria{Tags: []string{"missing:one", "category:two"}}, wantProbes: []model.CarrierProbe{anyDoc, missingTag, category}, message: `tag prefix "missing:" is not carried by any indexed committee document`},
		{name: "OR tags stop at first carried", criteria: model.SearchCriteria{Tags: []string{"category:one", "missing:two"}}, carried: []model.CarrierProbe{category}, wantProbes: []model.CarrierProbe{anyDoc, category}},
		{name: "bare tags skipped", criteria: model.SearchCriteria{Tags: []string{"active"}, TagsAll: []string{"current"}}},
		{name: "bare OR alternative retained", criteria: model.SearchCriteria{Tags: []string{"missing:one", "active"}}},
		{name: "bare OR alternative does not bypass AND", criteria: model.SearchCriteria{Tags: []string{"active"}, TagsAll: []string{"missing:one"}}, wantProbes: []model.CarrierProbe{anyDoc, missingTag}, message: `tag prefix "missing:" is not carried by any indexed committee document`},
		{name: "filter absent", criteria: model.SearchCriteria{Filters: []model.FieldFilter{{Field: "data.status", Value: "private-value"}}}, wantProbes: []model.CarrierProbe{anyDoc, status}, message: `filter field "status" is not carried by any indexed committee document`},
		{name: "AND filter absent", criteria: model.SearchCriteria{FiltersAll: []model.FieldFilter{{Field: "data.status", Value: "private-value"}}}, wantProbes: []model.CarrierProbe{anyDoc, status}, message: `filter field "status" is not carried by any indexed committee document`},
		{name: "OR filters one carried", criteria: model.SearchCriteria{FiltersOr: []model.FieldFilter{{Field: "data.missing"}, {Field: "data.status"}}}, carried: []model.CarrierProbe{status}, wantProbes: []model.CarrierProbe{anyDoc, missingField, status}},
		{name: "OR filters stop at first carried", criteria: model.SearchCriteria{FiltersOr: []model.FieldFilter{{Field: "data.status"}, {Field: "data.missing"}}}, carried: []model.CarrierProbe{status}, wantProbes: []model.CarrierProbe{anyDoc, status}},
		{name: "OR filters none carried", criteria: model.SearchCriteria{FiltersOr: []model.FieldFilter{{Field: "data.missing"}, {Field: "data.status"}}}, wantProbes: []model.CarrierProbe{anyDoc, missingField, status}, message: `filter field "missing" is not carried by any indexed committee document`},
		{name: "memoized carried prefix", criteria: model.SearchCriteria{TagsAll: []string{"category:one", "category:two"}, Tags: []string{"category:three"}}, carried: []model.CarrierProbe{category}, wantProbes: []model.CarrierProbe{anyDoc, category}},
		{name: "memoized absent prefix", criteria: model.SearchCriteria{Tags: []string{"missing:one", "missing:two"}}, wantProbes: []model.CarrierProbe{anyDoc, missingTag}, message: `tag prefix "missing:" is not carried by any indexed committee document`},
		{name: "memoized field across date and filters", criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateFrom: since, Filters: []model.FieldFilter{{Field: "data.start_time"}}, FiltersAll: []model.FieldFilter{{Field: "data.start_time"}}, FiltersOr: []model.FieldFilter{{Field: "data.start_time"}}}, carried: []model.CarrierProbe{date}, wantProbes: []model.CarrierProbe{anyDoc, date}},
		{name: "first failure is date", criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateFrom: since, Parent: stringPtr("project:value"), TagsAll: []string{"missing:value"}, Filters: []model.FieldFilter{{Field: "data.status"}}}, wantProbes: []model.CarrierProbe{anyDoc, date}, message: `date_field "start_time" is not carried by any indexed committee document`},
		{name: "disabled", disabled: true, criteria: model.SearchCriteria{DateField: stringPtr("data.start_time"), DateFrom: stringPtr("2025-01-01")}},
		{name: "probe outage", criteria: model.SearchCriteria{Parent: stringPtr("project:value")}, probeError: stderrors.New("unavailable"), wantProbes: []model.CarrierProbe{anyDoc}},
		{name: "probe validation is server error", criteria: model.SearchCriteria{Parent: stringPtr("project:value")}, probeError: errors.NewValidation("upstream query rejected"), wantProbes: []model.CarrierProbe{anyDoc}},
	} {
		for _, route := range []string{"search", "count", "grouped count", "metric count"} {
			for _, principal := range []string{"caller", constants.AnonymousPrincipal} {
				t.Run(tc.name+"/"+route+"/"+principal, func(t *testing.T) {
					criteria := tc.criteria
					if tc.name != "no type" && criteria.ResourceType == nil {
						criteria.ResourceType = stringPtr("committee")
					}
					searcher := mock.NewMockResourceSearcher()
					searcher.SetQueryResourcePages(&model.SearchResult{})
					searcher.SetCountPublicResponse(0)
					searcher.SetAccessBucketPages(&model.AccessBucketPage{})
					searcher.SetTypeCarries("committee", anyDoc, !tc.emptyType, tc.probeError)
					for _, probe := range tc.carried {
						searcher.SetTypeCarries("committee", probe, true, nil)
					}
					config := Config{DisableUnsatisfiableFilterRejection: tc.disabled}
					svc := newTestResourceSearchWithConfig(t, searcher, mock.NewMockAccessControlChecker(), config)
					ctx := context.WithValue(context.Background(), constants.PrincipalContextID, principal)
					var err error
					if route == "search" {
						var result *model.SearchResult
						result, err = svc.QueryResources(ctx, criteria)
						if err == nil {
							require.NotNil(t, result)
							assert.Empty(t, result.Resources)
						}
					} else {
						agg := model.CountAggregation{}
						if route == "grouped count" {
							agg.GroupByPrefix = "category"
						}
						if route == "metric count" {
							agg.CardinalityPrefix = "category"
						}
						var result *model.CountResult
						result, err = svc.QueryResourcesCount(ctx, criteria, criteria, agg)
						if err == nil {
							require.NotNil(t, result)
							assert.Zero(t, result.Count)
							if route == "grouped count" {
								require.NotNil(t, result.GroupsComplete)
								assert.True(t, *result.GroupsComplete)
							}
							if route == "metric count" {
								require.NotNil(t, result.MetricValue)
								assert.Zero(t, *result.MetricValue)
							}
						}
						if principal == constants.AnonymousPrincipal {
							assert.Zero(t, searcher.AccessBucketCalls())
						}
					}
					if tc.probeError != nil {
						var unavailable errors.ServiceUnavailable
						require.ErrorAs(t, err, &unavailable)
					} else if tc.message != "" {
						var validation errors.Validation
						require.ErrorAs(t, err, &validation)
						assert.EqualError(t, err, tc.message)
					} else {
						require.NoError(t, err)
					}
					calls := searcher.CarrierProbeCalls()
					require.Len(t, calls, len(tc.wantProbes))
					for i, probe := range tc.wantProbes {
						assert.Equal(t, mock.CarrierProbeCall{ResourceType: "committee", Probe: probe}, calls[i])
					}
				})
			}
		}
	}
}

func TestUnsatisfiableFiltersSkipNonzeroResults(t *testing.T) {
	for _, route := range []string{"search", "count public", "count private"} {
		t.Run(route, func(t *testing.T) {
			searcher := mock.NewMockResourceSearcher()
			searcher.SetTypeCarries("committee", model.CarrierProbe{Kind: model.AnyDocument}, true, nil)
			checker := mock.NewMockAccessControlChecker()
			checker.DefaultResult = "allowed"
			svc := newTestResourceSearch(t, searcher, checker)
			criteria := model.SearchCriteria{ResourceType: stringPtr("committee"), DateField: stringPtr("data.missing"), DateFrom: stringPtr("2025-01-01")}
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "caller")
			if route == "search" {
				searcher.SetQueryResourcePages(&model.SearchResult{Resources: []model.Resource{{TransactionBodyStub: model.TransactionBodyStub{Public: true}}}})
				_, err := svc.QueryResources(ctx, criteria)
				require.NoError(t, err)
			} else {
				searcher.SetCountPublicResponse(1)
				searcher.SetAccessBucketPages(&model.AccessBucketPage{})
				if route == "count private" {
					searcher.SetCountPublicResponse(0)
					searcher.SetAccessBucketPages(&model.AccessBucketPage{Buckets: []model.AggregationBucket{{Key: "committee:one#viewer", DocCount: 1}}})
				}
				result, err := svc.QueryResourcesCount(ctx, criteria, criteria, model.CountAggregation{})
				require.NoError(t, err)
				assert.Equal(t, 1, result.Count)
			}
			assert.Empty(t, searcher.CarrierProbeCalls())
		})
	}
}

func TestUnsatisfiableFiltersSkipDeniedWalkAndDirectGrantEmpty(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied then empty", true: "no direct grants"}[direct], func(t *testing.T) {
			searcher := mock.NewMockResourceSearcher()
			searcher.SetTypeCarries("b2b_org", model.CarrierProbe{Kind: model.AnyDocument}, true, nil)
			searcher.SetQueryResourcePages(page("next", privateOrg("one")), page(""))
			checker := mock.NewMockAccessControlChecker()
			checker.DefaultResult = "denied"
			svc := newTestResourceSearch(t, searcher, checker)
			criteria := model.SearchCriteria{ResourceType: stringPtr("b2b_org"), DateField: stringPtr("data.missing"), DateFrom: stringPtr("2025-01-01")}
			if direct {
				criteria.FilterGrants = stringPtr("direct")
			}
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "caller")
			result, err := svc.QueryResources(ctx, criteria)
			require.NoError(t, err)
			assert.Empty(t, result.Resources)
			assert.Empty(t, searcher.CarrierProbeCalls())
			if direct {
				assert.Zero(t, searcher.QueryResourceCalls())
			} else {
				assert.Equal(t, 2, searcher.QueryResourceCalls())
			}
		})
	}
}

func TestUnsatisfiableFiltersSkipContinuationPages(t *testing.T) {
	token := "opaque"
	cursor := `["a","1"]`
	for _, tc := range []struct {
		name     string
		criteria model.SearchCriteria
	}{
		{"page token", model.SearchCriteria{PageToken: &token}},
		{"search after cursor", model.SearchCriteria{SearchAfter: &cursor}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			searcher := mock.NewMockResourceSearcher()
			searcher.SetTypeCarries("committee", model.CarrierProbe{Kind: model.AnyDocument}, true, nil)
			searcher.SetQueryResourcePages(&model.SearchResult{})
			svc := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())
			criteria := tc.criteria
			criteria.ResourceType = stringPtr("committee")
			criteria.DateField = stringPtr("data.missing")
			criteria.DateFrom = stringPtr("2025-01-01")
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "caller")
			result, err := svc.QueryResources(ctx, criteria)
			require.NoError(t, err)
			assert.Empty(t, result.Resources)
			assert.Empty(t, searcher.CarrierProbeCalls())
		})
	}
}

func TestUnsatisfiableFiltersCancelledProbeIsNotUnavailable(t *testing.T) {
	searcher := mock.NewMockResourceSearcher()
	searcher.SetTypeCarries("committee", model.CarrierProbe{Kind: model.AnyDocument}, false, context.Canceled)
	searcher.SetQueryResourcePages(&model.SearchResult{})
	svc := newTestResourceSearch(t, searcher, mock.NewMockAccessControlChecker())
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), constants.PrincipalContextID, "caller"))
	cancel()
	_, err := svc.QueryResources(ctx, model.SearchCriteria{ResourceType: stringPtr("committee"), Parent: stringPtr("project:value")})
	require.ErrorIs(t, err, context.Canceled)
	var unavailable errors.ServiceUnavailable
	assert.False(t, stderrors.As(err, &unavailable))
}
