// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

const defaultHistoryIndex = "resource-history-v1"

var historyIndexPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// QueryHistory reads only mutation records from the independently protected
// history index. It does not use the resources index or its public bypass.
func (os *OpenSearchSearcher) QueryHistory(ctx context.Context, criteria model.HistoryCriteria) (*model.HistoryPage, error) {
	index := os.historyIndex
	if index == "" {
		index = defaultHistoryIndex
	}
	must := []any{}
	if criteria.ResourceType != nil {
		must = append(must, historyTerm("object_type", *criteria.ResourceType))
	}
	if criteria.Parent != nil {
		kind, uid, ok := strings.Cut(*criteria.Parent, ":")
		if !ok || !historyIndexPattern.MatchString(kind) || uid == "" {
			return nil, errors.NewServiceUnavailable("invalid history parent criteria")
		}
		// A parent timeline includes the referenced resource itself and
		// events captured before/after a move under that reference. Scope
		// membership is only a search predicate, never an access grant.
		self := map[string]any{"bool": map[string]any{"must": []any{
			historyTerm("object_type", kind), historyTerm("object_id", uid),
		}}}
		must = append(must, map[string]any{"bool": map[string]any{
			"should":               []any{historyTerm("scope.before", *criteria.Parent), historyTerm("scope.after", *criteria.Parent), self},
			"minimum_should_match": 1,
		}})
	}
	for _, filter := range append(append([]model.FieldFilter{}, criteria.Filters...), criteria.FiltersAll...) {
		must = append(must, historyTerm(filter.Field, filter.Value))
	}
	if len(criteria.FiltersOr) > 0 {
		choices := make([]any, 0, len(criteria.FiltersOr))
		for _, filter := range criteria.FiltersOr {
			choices = append(choices, historyTerm(filter.Field, filter.Value))
		}
		must = append(must, map[string]any{"bool": map[string]any{"should": choices, "minimum_should_match": 1}})
	}
	if criteria.DateField != nil && (criteria.DateFrom != nil || criteria.DateTo != nil) {
		bounds := make(map[string]string, 2)
		if criteria.DateFrom != nil {
			bounds["gte"] = *criteria.DateFrom
		}
		if criteria.DateTo != nil {
			bounds["lte"] = *criteria.DateTo
		}
		must = append(must, map[string]any{"range": map[string]any{*criteria.DateField: bounds}})
	}
	query := map[string]any{
		"size": criteria.PageSize,
		"query": map[string]any{"bool": map[string]any{
			"must": must, "must_not": historyTerm("erased", true),
		}},
		"sort": []any{
			map[string]any{"occurred_at": "asc"},
			map[string]any{"publisher": "asc"},
			map[string]any{"event_id": "asc"},
		},
	}
	if criteria.SearchAfter != nil {
		query["search_after"] = json.RawMessage(*criteria.SearchAfter)
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, errors.NewServiceUnavailable("history query could not be encoded")
	}
	response, err := os.client.SearchHistory(ctx, index, body)
	if err != nil {
		return nil, err
	}
	page := &model.HistoryPage{Events: make([]model.HistoryEvent, 0, len(response.Hits.Hits))}
	for _, hit := range response.Hits.Hits {
		var source struct {
			Publisher            string `json:"publisher"`
			EventID              string `json:"event_id"`
			ObjectType           string `json:"object_type"`
			ObjectID             string `json:"object_id"`
			Action               string `json:"action"`
			OccurredAt           string `json:"occurred_at"`
			ResourceRevision     uint64 `json:"resource_revision"`
			HistoryCheckObject   string `json:"history_check_object"`
			HistoryCheckRelation string `json:"history_check_relation"`
			Audit                struct {
				Attribution model.HistoryAttribution `json:"attribution"`
				Content     model.HistoryContent     `json:"content"`
			} `json:"audit"`
		}
		if len(hit.Source) == 0 || json.Unmarshal(hit.Source, &source) != nil ||
			source.Publisher == "" || source.EventID == "" || source.ObjectType == "" || source.ObjectID == "" ||
			source.ResourceRevision == 0 || source.Audit.Attribution.ActorRef == "" ||
			source.Audit.Attribution.EffectiveActorRef == "" || source.Audit.Content.Schema == "" ||
			source.Audit.Content.Representation != "snapshots" ||
			(source.Action != "created" && source.Action != "updated" && source.Action != "deleted") {
			return nil, errors.NewServiceUnavailable("private history document is incomplete")
		}
		if _, err := time.Parse(time.RFC3339Nano, source.OccurredAt); err != nil {
			return nil, errors.NewServiceUnavailable("private history occurrence time is invalid")
		}
		page.Events = append(page.Events, model.HistoryEvent{
			Publisher: source.Publisher, EventID: source.EventID,
			ObjectType: source.ObjectType, ObjectID: source.ObjectID,
			Action: source.Action, OccurredAt: source.OccurredAt,
			ResourceRevision: source.ResourceRevision,
			Attribution:      source.Audit.Attribution, Content: source.Audit.Content,
			HistoryCheckObject:   source.HistoryCheckObject,
			HistoryCheckRelation: source.HistoryCheckRelation,
		})
	}
	if len(response.Hits.Hits) == criteria.PageSize && criteria.PageSize > 0 {
		last := response.Hits.Hits[len(response.Hits.Hits)-1]
		if len(last.Sort) == 0 {
			return nil, errors.NewServiceUnavailable("private history cursor is missing")
		}
		cursor := string(last.Sort)
		page.SearchAfter = &cursor
	}
	return page, nil
}

func historyTerm(field string, value any) map[string]any {
	return map[string]any{"term": map[string]any{field: value}}
}
