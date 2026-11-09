// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/require"
)

type historySearchClient struct {
	*MockOpenSearchClient
	index string
	query map[string]any
}

func (c *historySearchClient) SearchHistory(_ context.Context, index string, query []byte) (*SearchResponse, error) {
	c.index = index
	if err := json.Unmarshal(query, &c.query); err != nil {
		return nil, err
	}
	return c.searchResponse, c.searchError
}

type historyPagingClient struct {
	*MockOpenSearchClient
	t    *testing.T
	hits []Hit
	call int
}

func (c *historyPagingClient) SearchHistory(_ context.Context, index string, body []byte) (*SearchResponse, error) {
	c.t.Helper()
	require.Equal(c.t, "resource-history-v1", index)
	var query struct {
		Size        int                 `json:"size"`
		Sort        []map[string]string `json:"sort"`
		SearchAfter json.RawMessage     `json:"search_after"`
	}
	require.NoError(c.t, json.Unmarshal(body, &query))
	require.Equal(c.t, 1, query.Size)
	require.Equal(c.t, []map[string]string{
		{"occurred_at": "asc"}, {"publisher": "asc"}, {"event_id": "asc"},
	}, query.Sort, "timestamp ties require both publisher and event ID as stable tie-breakers")
	if c.call == 0 {
		require.Empty(c.t, query.SearchAfter)
	} else {
		require.JSONEq(c.t, string(c.hits[c.call-1].Sort), string(query.SearchAfter), "continue after the last raw hit of the previous page")
	}
	var hits []Hit
	if c.call < len(c.hits) {
		hits = []Hit{c.hits[c.call]}
	}
	c.call++
	return &SearchResponse{Hits: Hits{Hits: hits}}, nil
}

func TestQueryHistoryTimestampTiePagination(t *testing.T) {
	const occurredAt = "2026-11-09T14:30:00Z"
	createHit := func(publisher, eventID string, revision int) Hit {
		t.Helper()
		source := fmt.Sprintf(`{"publisher":%q,"event_id":%q,"object_type":"fixture_resource","object_id":"same-resource","action":"updated","occurred_at":%q,"resource_revision":%d,"history_check_object":"fixture_resource:same-resource","history_check_relation":"auditor","audit":{"attribution":{"actor_ref":"service:worker","effective_actor_ref":"service:worker"},"content":{"schema":"fixture.audit.v1","representation":"snapshots","before":{"name":"Before"},"after":{"name":"After"}}}}`, publisher, eventID, occurredAt, revision)
		sort, err := json.Marshal([]string{occurredAt, publisher, eventID})
		require.NoError(t, err)
		return Hit{Source: json.RawMessage(source), Sort: sort}
	}
	client := &historyPagingClient{
		MockOpenSearchClient: NewMockOpenSearchClient(), t: t,
		hits: []Hit{
			createHit("alpha", "event-1", 1),
			createHit("alpha", "event-2", 2),
			createHit("beta", "event-1", 3),
		},
	}
	searcher := &OpenSearchSearcher{client: client, historyIndex: "resource-history-v1"}
	criteria := model.HistoryCriteria{SearchCriteria: model.SearchCriteria{PageSize: 1}}
	var identities []string
	for {
		page, err := searcher.QueryHistory(context.Background(), criteria)
		require.NoError(t, err)
		for _, event := range page.Events {
			require.Equal(t, "same-resource", event.ObjectID)
			identities = append(identities, event.Publisher+":"+event.EventID)
		}
		if page.SearchAfter == nil {
			break
		}
		criteria.SearchAfter = page.SearchAfter
	}
	require.Equal(t, []string{"alpha:event-1", "alpha:event-2", "beta:event-1"}, identities)
	require.Equal(t, 4, client.call, "a full final page needs an empty continuation page to prove exhaustion")
}

func TestQueryHistory(t *testing.T) {
	objectType := "committee_member"
	dateField, dateFrom := "occurred_at", "2026-11-01T00:00:00Z"
	cursor := `["2026-11-09T14:30:00Z","source","event-1"]`
	source := `{"publisher":"source","event_id":"event-1","object_type":"committee_member","object_id":"member-1","action":"updated","occurred_at":"2026-11-09T14:30:00Z","resource_revision":4,"history_check_object":"committee:one","history_check_relation":"auditor","audit":{"attribution":{"actor_ref":"service:worker","effective_actor_ref":"service:worker"},"content":{"schema":"fixture.audit.v1","representation":"snapshots","before":{"name":"Old"},"after":{"name":"New"}}}}`
	client := &historySearchClient{MockOpenSearchClient: NewMockOpenSearchClient()}
	client.searchResponse = &SearchResponse{Hits: Hits{Hits: []Hit{{Source: json.RawMessage(source), Sort: json.RawMessage(cursor)}}}}
	searcher := &OpenSearchSearcher{client: client, index: "resources", historyIndex: "resource-history-v1"}
	parent := "committee:one"
	criteria := model.HistoryCriteria{SearchCriteria: model.SearchCriteria{
		ResourceType: &objectType, Parent: &parent,
		Filters: []model.FieldFilter{{Field: "action", Value: "updated"}, {Field: "object_id", Value: "member-1"}}, DateField: &dateField,
		DateFrom: &dateFrom, PageSize: 1,
	}}
	page, err := searcher.QueryHistory(context.Background(), criteria)
	require.NoError(t, err)
	require.Equal(t, "resource-history-v1", client.index)
	require.Len(t, page.Events, 1)
	require.Equal(t, "member-1", page.Events[0].ObjectID)
	require.Equal(t, cursor, *page.SearchAfter)
	require.NotContains(t, stringMustMarshal(t, client.query), "latest")
	require.NotContains(t, stringMustMarshal(t, client.query), "public")
	require.Contains(t, stringMustMarshal(t, client.query), `"scope.before":"committee:one"`)
	require.Contains(t, stringMustMarshal(t, client.query), `"scope.after":"committee:one"`)
	require.Contains(t, stringMustMarshal(t, client.query), `"object_type":"committee"`)
	require.Contains(t, stringMustMarshal(t, client.query), `"object_id":"one"`)
	require.Contains(t, stringMustMarshal(t, client.query), `"object_id":"member-1"`)
	require.Zero(t, client.mappingCalls, "history parent must use existing indexed fields, not tags mapping")
	must := client.query["query"].(map[string]any)["bool"].(map[string]any)["must"].([]any)
	parentMatch := must[1].(map[string]any)["bool"].(map[string]any)
	require.Equal(t, float64(1), parentMatch["minimum_should_match"])
	require.Len(t, parentMatch["should"], 3, "before scope, after scope, and resource identity are alternatives")
	require.Contains(t, stringMustMarshal(t, client.query), `"erased":true`)
	criteria.SearchAfter = &cursor
	_, err = searcher.QueryHistory(context.Background(), criteria)
	require.NoError(t, err)
	require.Equal(t, []any{"2026-11-09T14:30:00Z", "source", "event-1"}, client.query["search_after"])

	client.searchResponse = &SearchResponse{Hits: Hits{Hits: []Hit{{Source: json.RawMessage(`{"action":"updated"}`)}}}}
	_, err = searcher.QueryHistory(context.Background(), criteria)
	var unavailable errors.ServiceUnavailable
	require.ErrorAs(t, err, &unavailable, "incomplete private source must fail the whole page")
}

func stringMustMarshal(t *testing.T, data any) string {
	t.Helper()
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	return string(encoded)
}
