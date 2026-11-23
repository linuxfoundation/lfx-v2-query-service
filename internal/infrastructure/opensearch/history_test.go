// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
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

func TestDecodeHistoryAttribution_RetainedIdentity(t *testing.T) {
	valid := `{"actor_ref":"user:testuser01","effective_actor_ref":"service:worker","actor_verified":true,"effective_actor_verified":false,"actor_identity":{"username":"testuser01","email":"user-1@example.com"}}`
	attribution, err := decodeHistoryAttribution([]byte(valid), "")
	require.NoError(t, err)
	require.True(t, attribution.ActorVerified)
	require.False(t, attribution.EffectiveActorVerified)
	require.Equal(t, "testuser01", *attribution.ActorIdentity.Username)
	require.Equal(t, "user-1@example.com", *attribution.ActorIdentity.Email)
	require.Nil(t, attribution.ActorProfile, "display must never be reconstructed from stored snapshots")
	require.Nil(t, attribution.EffectiveActorIdentity)
	for _, raw := range []string{
		`{"actor_ref":"user:testuser01","effective_actor_ref":"service:worker"}`,
		`{"actor_ref":"user:testuser01","effective_actor_ref":"service:worker","actor_identity":{"username":"other","email":null}}`,
		`{"actor_ref":"service:worker","effective_actor_ref":"service:worker","actor_identity":{"username":null,"email":null}}`,
		`{"actor_ref":"user:","effective_actor_ref":"service:worker"}`,
		strings.Replace(valid, `"actor_verified":true`, `"actor_verified":true,"actor_profile":null`, 1),
		strings.Replace(valid, `"actor_verified":true`, `"actor_verified":true,"actor_jwt":"private-marker"`, 1),
		strings.Replace(valid, `"username":"testuser01"`, `"username":"testuser01","name":"private-marker"`, 1),
	} {
		_, err := decodeHistoryAttribution([]byte(raw), "")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-marker")
	}
}

func TestDecodeHistoryAttribution_Legacy(t *testing.T) {
	for _, role := range []string{"actor", "effective_actor"} {
		for _, scenario := range []string{
			"valid", "missing origin", "live mutation", "verified", "missing verification", "null verification",
			"username", "email", "missing identity", "null identity", "missing username", "missing email", "empty identity",
			"JWT", "malformed source", "service missing verification", "service null verification",
		} {
			t.Run(role+"/"+scenario, func(t *testing.T) {
				identity := map[string]any{"username": nil, "email": nil}
				stored := map[string]any{
					"actor_ref": "service:fixture", "effective_actor_ref": "service:fixture",
					"actor_verified": false, "effective_actor_verified": false,
				}
				stored[role+"_ref"] = "legacy:v1-platform/actor-001"
				stored[role+"_identity"] = identity
				origin := constants.HistoryOriginHistoricalImport
				switch scenario {
				case "missing origin":
					origin = ""
				case "live mutation":
					origin = "mutation"
				case "verified":
					stored[role+"_verified"] = true
				case "missing verification":
					delete(stored, role+"_verified")
				case "null verification":
					stored[role+"_verified"] = nil
				case "username":
					identity["username"] = "first-last"
				case "email":
					identity["email"] = "first.last@example.com"
				case "missing identity":
					delete(stored, role+"_identity")
				case "null identity":
					stored[role+"_identity"] = nil
				case "missing username":
					delete(identity, "username")
				case "missing email":
					delete(identity, "email")
				case "empty identity":
					stored[role+"_identity"] = map[string]any{}
				case "JWT":
					stored[role+"_jwt"] = "private-marker"
				case "malformed source":
					stored[role+"_ref"] = "legacy:actor-001"
				case "service missing verification", "service null verification":
					other := "actor"
					if role == "actor" {
						other = "effective_actor"
					}
					if scenario == "service missing verification" {
						delete(stored, other+"_verified")
					} else {
						stored[other+"_verified"] = nil
					}
				}
				payload, err := json.Marshal(stored)
				require.NoError(t, err)
				attribution, err := decodeHistoryAttribution(payload, origin)
				if scenario == "valid" || strings.HasPrefix(scenario, "service ") {
					require.NoError(t, err)
					require.False(t, attribution.ActorVerified)
					require.False(t, attribution.EffectiveActorVerified)
					if role == "actor" {
						require.Equal(t, &model.HistoryIdentity{}, attribution.ActorIdentity)
					} else {
						require.Equal(t, &model.HistoryIdentity{}, attribution.EffectiveActorIdentity)
					}
				} else {
					require.ErrorContains(t, err, "private history attribution is invalid")
					require.NotContains(t, err.Error(), "private-marker")
				}
			})
		}
	}
}

func TestQueryHistory_LegacyOrigin(t *testing.T) {
	for _, origin := range []string{constants.HistoryOriginHistoricalImport, "mutation", ""} {
		t.Run(origin, func(t *testing.T) {
			source := fmt.Sprintf(`{"publisher":"source","event_id":"event-1","object_type":"committee_member","object_id":"member-1","action":"deleted","occurred_at":"2026-11-09T14:30:00Z","resource_revision":4,"history_check_object":"committee:one","history_check_relation":"auditor","audit":{"origin":%q,"attribution":{"actor_ref":"legacy:v1-platform/actor-001","effective_actor_ref":"legacy:v1-platform/actor-001","actor_verified":false,"effective_actor_verified":false,"actor_identity":{"username":null,"email":null},"effective_actor_identity":{"username":null,"email":null}},"content":{"schema":"fixture.audit.v1","representation":"snapshots","before":{"name":"Before"}}}}`, origin)
			client := &historySearchClient{MockOpenSearchClient: NewMockOpenSearchClient()}
			client.searchResponse = &SearchResponse{Hits: Hits{Hits: []Hit{{Source: json.RawMessage(source)}}}}
			page, err := (&OpenSearchSearcher{client: client}).QueryHistory(context.Background(), model.HistoryCriteria{})
			if origin == constants.HistoryOriginHistoricalImport {
				require.NoError(t, err)
				require.Len(t, page.Events, 1)
				require.Equal(t, "legacy:v1-platform/actor-001", page.Events[0].Attribution.ActorRef)
			} else {
				require.Error(t, err)
				require.Nil(t, page)
			}
		})
	}
}

func TestDecodeHistoryAttribution_OlderActorFlags(t *testing.T) {
	for _, value := range []string{"omitted", "null"} {
		t.Run(value, func(t *testing.T) {
			stored := map[string]any{
				"actor_ref": "user:first-last", "effective_actor_ref": "service:worker",
				"actor_identity": map[string]any{"username": "first-last"},
			}
			if value == "null" {
				stored["actor_verified"], stored["effective_actor_verified"] = nil, nil
			}
			payload, err := json.Marshal(stored)
			require.NoError(t, err)
			attribution, err := decodeHistoryAttribution(payload, "")
			require.NoError(t, err)
			require.False(t, attribution.ActorVerified)
			require.False(t, attribution.EffectiveActorVerified)
			require.Equal(t, "first-last", *attribution.ActorIdentity.Username)
			require.Nil(t, attribution.ActorIdentity.Email)
			require.Nil(t, attribution.EffectiveActorIdentity)
		})
	}
}
