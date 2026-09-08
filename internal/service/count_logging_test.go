// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestCountLogsDoNotExposeBatchesOrValues(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	searcher := mock.NewMockResourceSearcher()
	searcher.ClearResources()
	sentinel := "sensitive-marker@example.com"
	resource := mock.NewResourceWithDefaults("probe", sentinel, map[string]any{"name": sentinel, "tags": []string{"email:" + sentinel}}, false)
	searcher.AddResource(resource)
	checker := mock.NewMockAccessControlChecker()
	service := newTestResourceSearch(t, searcher, checker)
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	public := model.SearchCriteria{PublicOnly: true, PageSize: -1, Name: &sentinel, TagsAll: []string{"email:" + sentinel}}
	private := public
	private.PublicOnly = false
	private.PrivateOnly = true
	private.PageSize = 0
	result, err := service.QueryResourcesCount(ctx, public, private, model.CountAggregation{GroupByPrefix: "email", GroupBySize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	checker.SetCheckAccessError(errors.New("checker unavailable"))
	_, err = service.QueryResourcesCount(ctx, public, private, model.CountAggregation{})
	require.Error(t, err)
	require.Contains(t, logs.String(), `"error":"checker unavailable"`, "retain the failure reason without the batch")
	require.NotContains(t, logs.String(), sentinel)
	require.Contains(t, logs.String(), `"bucket_count":1`)
	require.Contains(t, logs.String(), `"response_count":1`)
}
