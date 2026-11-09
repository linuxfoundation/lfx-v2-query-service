// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/require"
)

type historyPageSearcher struct {
	port.ResourceSearcher
	pages   []*model.HistoryPage
	calls   int
	cursors []string
	err     error
}

func (s *historyPageSearcher) QueryHistory(_ context.Context, criteria model.HistoryCriteria) (*model.HistoryPage, error) {
	s.calls++
	if criteria.SearchAfter != nil {
		s.cursors = append(s.cursors, *criteria.SearchAfter)
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(s.pages) == 0 {
		return &model.HistoryPage{}, nil
	}
	page := s.pages[0]
	s.pages = s.pages[1:]
	return page, nil
}

type historyChecker struct {
	port.AccessControlChecker
	requests []string
	allowed  map[string]bool
	err      error
}

func (c *historyChecker) MaxPayload() int64 { return 0 }
func (c *historyChecker) CheckAccess(_ context.Context, _ string, body []byte, _ time.Duration) (model.AccessCheckResult, error) {
	if c.err != nil {
		return nil, c.err
	}
	result := model.AccessCheckResult{}
	for _, line := range strings.Split(string(body), "\n") {
		if line != "" {
			c.requests = append(c.requests, line)
			if c.allowed[line] {
				result[line] = "true"
			} else {
				result[line] = "false"
			}
		}
	}
	return result, nil
}

func TestQueryResourcesHistory(t *testing.T) {
	auditor := "committee:one#auditor@user:reader"
	other := "committee:other#auditor@user:reader"
	cursor := `["2026-11-09T14:30:00Z","source","event-1"]`
	events := []model.HistoryEvent{
		{EventID: "1", HistoryCheckObject: "committee:one", HistoryCheckRelation: "auditor"},
		{EventID: "2", HistoryCheckObject: "committee:one", HistoryCheckRelation: "auditor"},
		{EventID: "3", HistoryCheckObject: "committee:one", HistoryCheckRelation: "viewer"},
		{EventID: "4", HistoryCheckObject: "committee:other", HistoryCheckRelation: "auditor"},
		{EventID: "5", HistoryCheckObject: "", HistoryCheckRelation: "auditor"},
		{EventID: "6", HistoryCheckObject: "committee:one\nfoo", HistoryCheckRelation: "auditor"},
	}
	for _, tc := range []struct {
		name      string
		pages     []*model.HistoryPage
		allowed   map[string]bool
		principal string
		failure   error
		want      []string
		calls     int
		checks    int
		wantError bool
	}{
		{"auditor-only and deduplicated", []*model.HistoryPage{{Events: events}}, map[string]bool{auditor: true}, "reader", nil, []string{"1", "2"}, 1, 2, false},
		{"viewer grant does not grant history", []*model.HistoryPage{{Events: events}}, nil, "reader", nil, []string{}, 1, 2, false},
		{"denied page is walked", []*model.HistoryPage{{Events: []model.HistoryEvent{events[3]}, SearchAfter: &cursor}, {Events: events[:1]}}, map[string]bool{auditor: true}, "reader", nil, []string{"1"}, 2, 2, false},
		{"anonymous rejected before search", nil, nil, constants.AnonymousPrincipal, nil, nil, 0, 0, true},
		{"access failure fails whole page", []*model.HistoryPage{{Events: events[:1]}}, nil, "reader", fmt.Errorf("unavailable"), nil, 1, 0, true},
		{"cross-project isolation", []*model.HistoryPage{{Events: events}}, map[string]bool{other: true}, "reader", nil, []string{"4"}, 1, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			searcher := &historyPageSearcher{pages: tc.pages}
			checker := &historyChecker{allowed: tc.allowed, err: tc.failure}
			svc, err := NewResourceSearch(searcher, checker, mock.NewMockResourceFilter(), DefaultConfig())
			require.NoError(t, err)
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, tc.principal)
			result, err := svc.QueryResourcesHistory(ctx, model.HistoryCriteria{SearchCriteria: model.SearchCriteria{PageSize: 50}})
			if tc.wantError {
				require.Error(t, err)
				if tc.failure != nil {
					var unavailable errors.ServiceUnavailable
					require.ErrorAs(t, err, &unavailable)
				}
			} else {
				require.NoError(t, err)
				ids := make([]string, 0, len(result.Events))
				for _, event := range result.Events {
					ids = append(ids, event.EventID)
				}
				require.Equal(t, tc.want, ids)
			}
			require.Equal(t, tc.calls, searcher.calls)
			require.Len(t, checker.requests, tc.checks)
		})
	}
}

func TestQueryResourcesHistoryRevocation(t *testing.T) {
	searcher := &historyPageSearcher{pages: []*model.HistoryPage{
		{Events: []model.HistoryEvent{{EventID: "1", HistoryCheckObject: "committee:one", HistoryCheckRelation: "auditor"}}},
		{Events: []model.HistoryEvent{{EventID: "1", HistoryCheckObject: "committee:one", HistoryCheckRelation: "auditor"}}},
	}}
	checker := &historyChecker{allowed: map[string]bool{"committee:one#auditor@user:reader": true}}
	svc, err := NewResourceSearch(searcher, checker, mock.NewMockResourceFilter(), DefaultConfig())
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "reader")
	first, err := svc.QueryResourcesHistory(ctx, model.HistoryCriteria{SearchCriteria: model.SearchCriteria{PageSize: 50}})
	require.NoError(t, err)
	require.Len(t, first.Events, 1)
	delete(checker.allowed, "committee:one#auditor@user:reader")
	second, err := svc.QueryResourcesHistory(ctx, model.HistoryCriteria{SearchCriteria: model.SearchCriteria{PageSize: 50}})
	require.NoError(t, err)
	require.Empty(t, second.Events)
}
