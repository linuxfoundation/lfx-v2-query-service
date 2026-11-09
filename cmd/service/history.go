// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/global"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/paging"
)

const historyCacheControl = "private, no-store"

var historyParentTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type historyPageToken struct {
	Version int             `json:"version"`
	Scope   string          `json:"scope"`
	Cursor  json.RawMessage `json:"cursor"`
}

// QueryResourcesHistory returns only independently authorized mutation records.
func (s *querySvcsrvc) QueryResourcesHistory(ctx context.Context, p *querysvc.QueryResourcesHistoryPayload) (*querysvc.QueryResourcesHistoryResult, error) {
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok || principal == "" || principal == constants.AnonymousPrincipal {
		return nil, wrapError(ctx, errors.NewValidation("history requires an authenticated principal"))
	}
	criteria, scope, err := historyCriteria(ctx, p, principal)
	if err != nil {
		return nil, wrapError(ctx, err)
	}
	result, err := s.resourceService.QueryResourcesHistory(ctx, criteria)
	if err != nil {
		return nil, wrapError(ctx, err)
	}
	response := &querysvc.QueryResourcesHistoryResult{
		Events:       make([]*querysvc.HistoryEvent, 0, len(result.Events)),
		CacheControl: historyCacheControl,
	}
	for _, event := range result.Events {
		response.Events = append(response.Events, &querysvc.HistoryEvent{
			EventID:  event.Publisher + ":" + event.EventID,
			Resource: &querysvc.HistoryResource{Type: event.ObjectType, UID: event.ObjectID},
			Action:   event.Action, OccurredAt: event.OccurredAt,
			ResourceRevision: event.ResourceRevision,
			Attribution:      event.Attribution, Content: event.Content,
		})
	}
	if result.SearchAfter != nil {
		token, err := paging.EncodePageToken(historyPageToken{Version: 1, Scope: scope, Cursor: json.RawMessage(*result.SearchAfter)}, global.PageTokenSecret(ctx))
		if err != nil {
			return nil, wrapError(ctx, err)
		}
		response.PageToken = &token
	}
	return response, nil
}

// historyCriteria validates history-only indexed dimensions, then binds the
// continuation to the complete normalized query and principal. No caller can
// select an FGA relation, or filter inside unindexed personal snapshots.
func historyCriteria(ctx context.Context, p *querysvc.QueryResourcesHistoryPayload, principal string) (model.HistoryCriteria, string, error) {
	criteria := model.HistoryCriteria{SearchCriteria: model.SearchCriteria{
		ResourceType: p.Type, Parent: p.Parent, PageSize: p.PageSize,
	}}
	if p.Type == nil && p.Parent == nil {
		return criteria, "", errors.NewValidation("at least one search parameter must be provided: type or parent")
	}
	if p.Type != nil && !historyParentTypePattern.MatchString(*p.Type) {
		return criteria, "", errors.NewValidation("invalid history type")
	}
	if p.Parent != nil {
		kind, uid, ok := strings.Cut(*p.Parent, ":")
		if !ok || !historyParentTypePattern.MatchString(kind) || uid == "" || len(uid) > 256 {
			return criteria, "", errors.NewValidation("parent must be a resource reference (type:uid)")
		}
		for i := 0; i < len(uid); i++ {
			if uid[i] < 0x21 || uid[i] > 0x7e || strings.ContainsRune(":#@{}", rune(uid[i])) {
				return criteria, "", errors.NewValidation("parent must be a resource reference (type:uid)")
			}
		}
	}
	if p.PageSize < 1 || p.PageSize > 100 {
		return criteria, "", errors.NewValidation("page_size must be between 1 and 100")
	}
	sets := [][]string{p.Filters, p.FiltersAll, p.FiltersOr}
	parsed := make([][]model.FieldFilter, 0, len(sets))
	for _, set := range sets {
		filters := make([]model.FieldFilter, 0, len(set))
		for _, raw := range set {
			field, value, ok := strings.Cut(raw, ":")
			if !ok || !validHistoryFilter(field, value) {
				return criteria, "", errors.NewValidation("history filters support action:created|updated|deleted or object_id:<uid>")
			}
			filters = append(filters, model.FieldFilter{Field: field, Value: value})
		}
		parsed = append(parsed, filters)
	}
	criteria.Filters, criteria.FiltersAll, criteria.FiltersOr = parsed[0], parsed[1], parsed[2]
	if p.DateField != nil && *p.DateField != "occurred_at" {
		return criteria, "", errors.NewValidation("history date_field must be occurred_at")
	}
	if (p.DateFrom != nil || p.DateTo != nil) && p.DateField == nil {
		return criteria, "", errors.NewValidation("date_field is required when using date_from or date_to")
	}
	criteria.DateField = p.DateField
	if p.DateFrom != nil {
		date, err := parseHistoryDateFilter(*p.DateFrom, false)
		if err != nil {
			return criteria, "", errors.NewValidation("invalid date_from", err)
		}
		criteria.DateFrom = &date
	}
	if p.DateTo != nil {
		date, err := parseHistoryDateFilter(*p.DateTo, true)
		if err != nil {
			return criteria, "", errors.NewValidation("invalid date_to", err)
		}
		criteria.DateTo = &date
	}
	// Do not put personal identifiers into token plaintext or log output.
	sealedScope, err := json.Marshal(struct {
		Principal string
		Criteria  model.SearchCriteria
	}{principal, criteria.SearchCriteria})
	if err != nil {
		return criteria, "", errors.NewValidation("invalid history query")
	}
	digest := sha256.Sum256(sealedScope)
	scope := hex.EncodeToString(digest[:])
	if p.PageToken != nil {
		if len(*p.PageToken) > 4096 {
			return criteria, "", errors.NewValidation("invalid history page token")
		}
		decoded, err := paging.DecodePageToken(ctx, *p.PageToken, global.PageTokenSecret(ctx))
		if err != nil {
			return criteria, "", err
		}
		decoder := json.NewDecoder(bytes.NewBufferString(decoded))
		decoder.DisallowUnknownFields()
		var token historyPageToken
		if decoder.Decode(&token) != nil || token.Version != 1 || token.Scope != scope {
			return criteria, "", errors.NewValidation("history page_token does not match this read")
		}
		var cursor []json.RawMessage
		if json.Unmarshal(token.Cursor, &cursor) != nil || len(cursor) != 3 {
			return criteria, "", errors.NewValidation("invalid history page token cursor")
		}
		var publisher, eventID string
		if json.Unmarshal(cursor[1], &publisher) != nil || publisher == "" || json.Unmarshal(cursor[2], &eventID) != nil || eventID == "" {
			return criteria, "", errors.NewValidation("invalid history page token cursor")
		}
		var dateString string
		if json.Unmarshal(cursor[0], &dateString) == nil {
			if _, err := time.Parse(time.RFC3339Nano, dateString); err != nil {
				return criteria, "", errors.NewValidation("invalid history page token time")
			}
		} else if millis, err := strconv.ParseInt(string(cursor[0]), 10, 64); err != nil || millis < 0 {
			return criteria, "", errors.NewValidation("invalid history page token time")
		}
		searchAfter := string(token.Cursor)
		criteria.SearchAfter = &searchAfter
	}
	return criteria, scope, nil
}

// parseHistoryDateFilter accepts only the precision the history index can
// distinguish. Errors must not include the supplied value: history filters can
// identify people and the HTTP boundary logs validation errors.
func parseHistoryDateFilter(value string, end bool) (string, error) {
	if timestamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		if timestamp.Nanosecond()%int(time.Millisecond) != 0 {
			return "", fmt.Errorf("history date bounds support millisecond precision at most")
		}
		return timestamp.Format(time.RFC3339Nano), nil
	}
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", fmt.Errorf("history date bounds must be RFC3339 (up to milliseconds) or YYYY-MM-DD")
	}
	if end {
		return day.Add(24*time.Hour - time.Millisecond).Format(time.RFC3339Nano), nil
	}
	return day.Format(time.RFC3339), nil
}

func validHistoryFilter(field, value string) bool {
	if field == "action" {
		return value == "created" || value == "updated" || value == "deleted"
	}
	if field != "object_id" || value == "" || len(value) > 256 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e || strings.ContainsRune(":#@{}", rune(value[i])) {
			return false
		}
	}
	return true
}
