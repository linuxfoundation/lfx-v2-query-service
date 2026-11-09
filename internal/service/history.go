// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"regexp"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

var historyObjectTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var historyRelationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validHistoryObject(ref string) bool {
	kind, uid, ok := strings.Cut(ref, ":")
	if !ok || !historyObjectTypePattern.MatchString(kind) || uid == "" || len(uid) > 256 {
		return false
	}
	for i := 0; i < len(uid); i++ {
		b := uid[i]
		if b < 0x21 || b > 0x7e || strings.ContainsRune(":#@{}", rune(b)) {
			return false
		}
	}
	return true
}

// QueryResourcesHistory reads raw mutation pages and checks every event, even
// if the corresponding current resource is public or has been removed. The
// token always follows raw hits, not the smaller authorized page.
func (s *ResourceSearch) QueryResourcesHistory(ctx context.Context, criteria model.HistoryCriteria) (*model.HistoryResult, error) {
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok || principal == "" || principal == constants.AnonymousPrincipal {
		return nil, errors.NewValidation("history requires an authenticated principal")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.SearchRequestTimeout)
	defer cancel()

	result := &model.HistoryResult{Events: []model.HistoryEvent{}}
	for fetched := 0; ; fetched++ {
		page, err := s.resourceSearcher.QueryHistory(ctx, criteria)
		if err != nil {
			var validation errors.Validation
			if stderrors.As(err, &validation) {
				return nil, err
			}
			return nil, errors.NewServiceUnavailable("history search unavailable", err)
		}
		if page == nil {
			return nil, errors.NewServiceUnavailable("history search returned no page")
		}
		message := make([]byte, 0, len(page.Events)*100)
		seen := make(map[string]bool, len(page.Events))
		for _, event := range page.Events {
			// These values are selected from the private history document, never
			// from request filters, public visibility, or its actor attribution.
			if !validHistoryObject(event.HistoryCheckObject) || !historyRelationPattern.MatchString(event.HistoryCheckRelation) || event.HistoryCheckRelation == "viewer" {
				continue
			}
			key := event.HistoryCheckObject + "#" + event.HistoryCheckRelation + "@user:" + principal
			if !seen[key] {
				seen[key] = true
				message = append(message, key...)
				message = append(message, '\n')
			}
		}
		grants, err := s.sendAccessCheckBatch(ctx, message)
		if err != nil {
			return nil, errors.NewServiceUnavailable("history authorization unavailable", err)
		}
		for _, event := range page.Events {
			key := event.HistoryCheckObject + "#" + event.HistoryCheckRelation + "@user:" + principal
			if validHistoryObject(event.HistoryCheckObject) && historyRelationPattern.MatchString(event.HistoryCheckRelation) && event.HistoryCheckRelation != "viewer" && grants[key] == "true" {
				result.Events = append(result.Events, event)
			}
		}
		result.SearchAfter = page.SearchAfter
		if len(result.Events) > 0 || page.SearchAfter == nil || fetched >= s.config.DeniedPageWalk {
			return result, nil
		}
		if criteria.SearchAfter != nil && *criteria.SearchAfter == *page.SearchAfter {
			return nil, errors.NewServiceUnavailable("history cursor did not advance")
		}
		if ctx.Err() != nil {
			return nil, errors.NewServiceUnavailable("history search timed out", ctx.Err())
		}
		criteria.SearchAfter = page.SearchAfter
	}
}
