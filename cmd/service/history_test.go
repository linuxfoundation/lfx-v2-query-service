// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
	"github.com/stretchr/testify/require"
)

func TestHistoryCriteria(t *testing.T) {
	t.Setenv("PAGE_TOKEN_SECRET", "12345678901234567890123456789012")
	ctx := context.Background()
	resourceType, parent, field, from := "committee_member", "committee:one", "occurred_at", "2026-11-01"
	p := &querysvc.QueryResourcesHistoryPayload{
		Type: &resourceType, Parent: &parent,
		Filters: []string{"action:updated", "object_id:member-1"}, DateField: &field, DateFrom: &from, PageSize: 50,
	}
	criteria, scope, err := historyCriteria(ctx, p, "reader")
	require.NoError(t, err)
	require.Equal(t, "action", criteria.Filters[0].Field)
	require.Equal(t, "2026-11-01T00:00:00Z", *criteria.DateFrom)
	require.NotEmpty(t, scope)

	cursor := json.RawMessage(`["2026-11-09T14:30:00Z","source","event-1"]`)
	token, err := paging.EncodePageToken(historyPageToken{Version: 1, Scope: scope, Cursor: cursor}, global.PageTokenSecret(ctx))
	require.NoError(t, err)
	p.PageToken = &token
	continued, _, err := historyCriteria(ctx, p, "reader")
	require.NoError(t, err)
	require.JSONEq(t, string(cursor), *continued.SearchAfter)
	// OpenSearch's date mapping normally returns epoch-millisecond sort values.
	numeric, err := paging.EncodePageToken(historyPageToken{Version: 1, Scope: scope, Cursor: json.RawMessage(`[1794253800000,"source","event-1"]`)}, global.PageTokenSecret(ctx))
	require.NoError(t, err)
	p.PageToken = &numeric
	_, _, err = historyCriteria(ctx, p, "reader")
	require.NoError(t, err)
	p.PageToken = &token

	for _, tc := range []struct {
		name      string
		edit      func(*querysvc.QueryResourcesHistoryPayload)
		principal string
	}{
		{"changed principal", func(*querysvc.QueryResourcesHistoryPayload) {}, "other"},
		{"changed parent", func(p *querysvc.QueryResourcesHistoryPayload) { changed := "committee:two"; p.Parent = &changed }, "reader"},
		{"changed resource", func(p *querysvc.QueryResourcesHistoryPayload) {
			p.Filters = []string{"action:updated", "object_id:member-2"}
		}, "reader"},
		{"changed action", func(p *querysvc.QueryResourcesHistoryPayload) { p.Filters = []string{"action:deleted"} }, "reader"},
		{"changed date", func(p *querysvc.QueryResourcesHistoryPayload) { d := "2026-11-02"; p.DateFrom = &d }, "reader"},
		{"changed page size", func(p *querysvc.QueryResourcesHistoryPayload) { p.PageSize = 10 }, "reader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *p
			tc.edit(&copy)
			_, _, err := historyCriteria(ctx, &copy, tc.principal)
			require.Error(t, err)
		})
	}
	for _, invalid := range []struct {
		name    string
		payload querysvc.QueryResourcesHistoryPayload
	}{
		{"unscoped", querysvc.QueryResourcesHistoryPayload{PageSize: 50}},
		{"bad action", querysvc.QueryResourcesHistoryPayload{Type: &resourceType, Filters: []string{"action:viewer"}, PageSize: 50}},
		{"bad parent", querysvc.QueryResourcesHistoryPayload{Parent: &resourceType, PageSize: 50}},
		{"invalid parent injection", querysvc.QueryResourcesHistoryPayload{Parent: stringPtr("committee:one\nfoo"), PageSize: 50}},
		{"invalid resource id", querysvc.QueryResourcesHistoryPayload{Type: &resourceType, Filters: []string{"object_id:member\nother"}, PageSize: 50}},
		{"unindexed field", querysvc.QueryResourcesHistoryPayload{Type: &resourceType, Filters: []string{"audit.attribution.actor_ref:user:a"}, PageSize: 50}},
		{"date without field", querysvc.QueryResourcesHistoryPayload{Type: &resourceType, DateFrom: &from, PageSize: 50}},
		{"unsupported date field", querysvc.QueryResourcesHistoryPayload{Type: &resourceType, DateField: &resourceType, PageSize: 50}},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			_, _, err := historyCriteria(ctx, &invalid.payload, "reader")
			require.Error(t, err)
		})
	}
	foreignToken, err := paging.EncodePageToken([]string{"sort-name", "id"}, global.PageTokenSecret(ctx))
	require.NoError(t, err)
	p.PageToken = &foreignToken
	_, _, err = historyCriteria(ctx, p, "reader")
	require.Error(t, err)
}

func TestHistoryCriteriaPreservesFractionalDateBounds(t *testing.T) {
	field, from, to := "occurred_at", "2026-11-09T14:30:00.500Z", "2026-11-09T14:30:00.750Z"
	parent := "committee:one"
	criteria, _, err := historyCriteria(context.Background(), &querysvc.QueryResourcesHistoryPayload{
		Parent: &parent, DateField: &field, DateFrom: &from, DateTo: &to, PageSize: 50,
	}, "reader")
	require.NoError(t, err)
	require.Equal(t, "2026-11-09T14:30:00.5Z", *criteria.DateFrom, "date_from must not admit earlier events")
	require.Equal(t, "2026-11-09T14:30:00.75Z", *criteria.DateTo, "date_to must include events through the requested fraction")

	day := "2026-11-09"
	criteria, _, err = historyCriteria(context.Background(), &querysvc.QueryResourcesHistoryPayload{
		Parent: &parent, DateField: &field, DateFrom: &day, DateTo: &day, PageSize: 50,
	}, "reader")
	require.NoError(t, err)
	require.Equal(t, "2026-11-09T00:00:00Z", *criteria.DateFrom)
	require.Equal(t, "2026-11-09T23:59:59.999Z", *criteria.DateTo, "date-only end must include the final indexed millisecond")
	lastSecondEvent, err := time.Parse(time.RFC3339Nano, "2026-11-09T23:59:59.500Z")
	require.NoError(t, err)
	endBound, err := time.Parse(time.RFC3339Nano, *criteria.DateTo)
	require.NoError(t, err)
	require.False(t, lastSecondEvent.After(endBound), "the inclusive end must retain events in the last second")
}

func TestHistoryCriteriaRejectsUnsupportedOrPrivateDateValues(t *testing.T) {
	parent, field := "committee:one", "occurred_at"
	for _, tc := range []struct {
		name string
		from *string
		to   *string
	}{
		{"sub-millisecond lower bound", stringPtr("2026-11-09T14:30:00.500100Z"), nil},
		{"sub-millisecond upper bound", nil, stringPtr("2026-11-09T14:30:00.500900Z")},
		{"invalid private lower input", stringPtr("alice@example.org"), nil},
		{"invalid private upper input", nil, stringPtr("private@example.org")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := &querysvc.QueryResourcesHistoryPayload{
				Parent: &parent, DateField: &field, DateFrom: tc.from, DateTo: tc.to, PageSize: 50,
			}
			_, _, err := historyCriteria(context.Background(), payload, "reader")
			require.Error(t, err)
			if tc.from != nil {
				require.NotContains(t, err.Error(), *tc.from)
			}
			if tc.to != nil {
				require.NotContains(t, err.Error(), *tc.to)
			}
			var validation errors.Validation
			require.ErrorAs(t, err, &validation)
		})
	}
}

func TestQueryResourcesHistoryAnonymous(t *testing.T) {
	svc := &querySvcsrvc{}
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, constants.AnonymousPrincipal)
	_, err := svc.QueryResourcesHistory(ctx, &querysvc.QueryResourcesHistoryPayload{})
	require.Error(t, err)
}

type historyEndpointSearcher struct{ port.ResourceSearcher }

func (historyEndpointSearcher) QueryHistory(context.Context, model.HistoryCriteria) (*model.HistoryPage, error) {
	return &model.HistoryPage{Events: []model.HistoryEvent{{
		Publisher: "source", EventID: "event-1", ObjectType: "committee_member", ObjectID: "member-1",
		Action: "deleted", OccurredAt: "2026-11-09T14:30:00Z", ResourceRevision: 4,
		HistoryCheckObject: "committee:one", HistoryCheckRelation: "auditor",
		Attribution: model.HistoryAttribution{ActorRef: "service:worker", EffectiveActorRef: "service:worker"},
		Content:     model.HistoryContent{Schema: "fixture.audit.v1", Representation: "snapshots", Before: json.RawMessage(`{"name":"Old"}`)},
	}}}, nil
}

type historyEndpointChecker struct{ port.AccessControlChecker }

func (historyEndpointChecker) MaxPayload() int64 { return 0 }
func (historyEndpointChecker) CheckAccess(_ context.Context, _ string, _ []byte, _ time.Duration) (model.AccessCheckResult, error) {
	return model.AccessCheckResult{"committee:one#auditor@user:reader": "true"}, nil
}

func TestQueryResourcesHistoryResponse(t *testing.T) {
	resourceSvc, err := service.NewResourceSearch(historyEndpointSearcher{}, historyEndpointChecker{}, mock.NewMockResourceFilter(), service.DefaultConfig())
	require.NoError(t, err)
	svc := &querySvcsrvc{resourceService: resourceSvc}
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "reader")
	resourceType := "committee_member"
	result, err := svc.QueryResourcesHistory(ctx, &querysvc.QueryResourcesHistoryPayload{Type: &resourceType, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, "private, no-store", result.CacheControl)
	require.Len(t, result.Events, 1)
	require.Equal(t, "source:event-1", result.Events[0].EventID)
	require.Equal(t, "member-1", result.Events[0].Resource.UID)
	require.Equal(t, "deleted", result.Events[0].Action)
	encoded, err := json.Marshal(result.Events[0].Content)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"before"`)
	require.NotContains(t, string(encoded), `"after"`)
}
