// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedSearcher scripts raw OpenSearch pages keyed by the incoming page
// token (the first page is keyed by ""), the way the real searcher pages with
// search_after. It embeds the mock only to satisfy the rest of the port.
type pagedSearcher struct {
	*mock.MockResourceSearcher
	pages map[string]*model.SearchResult
	calls []string
}

func (p *pagedSearcher) QueryResources(_ context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {
	key := ""
	if criteria.PageToken != nil {
		key = *criteria.PageToken
	}
	p.calls = append(p.calls, key)
	page, ok := p.pages[key]
	if !ok {
		return &model.SearchResult{Resources: []model.Resource{}}, nil
	}
	// Copy so the service's in-place filtering cannot leak between calls.
	out := &model.SearchResult{PageToken: page.PageToken}
	out.Resources = append(out.Resources, page.Resources...)
	return out, nil
}

func privateOrg(id string) model.Resource {
	return model.Resource{
		Type: "b2b_org",
		ID:   id,
		Data: map[string]any{"name": id},
		TransactionBodyStub: model.TransactionBodyStub{
			ObjectRef:           "b2b_org:" + id,
			ObjectType:          "b2b_org",
			ObjectID:            id,
			AccessCheckObject:   "b2b_org:" + id,
			AccessCheckRelation: "auditor",
		},
	}
}

func tokenPtr(s string) *string { return &s }

// TestResourceSearchQueryResources_DeniedPagesDoNotLeakExistence pins the
// contract that an authenticated caller cannot tell "matches exist but you may
// not see them" from "nothing matches": both come back as an empty page with
// no page_token. Before the denied-page walk, the former returned a token
// (minted from the raw hits before the access check) and the latter did not —
// an existence oracle for any exact-tag lookup such as an organization slug.
func TestResourceSearchQueryResources_DeniedPagesDoNotLeakExistence(t *testing.T) {
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "viewer")
	slugTag := model.SearchCriteria{Tags: []string{"slug:acme-corp"}, PageSize: 1}

	newService := func(t *testing.T, pages map[string]*model.SearchResult, denied []string, walk int) (*ResourceSearch, *pagedSearcher) {
		t.Helper()
		searcher := &pagedSearcher{MockResourceSearcher: mock.NewMockResourceSearcher(), pages: pages}
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		checker.DeniedResourceIDs = denied
		config := DefaultConfig()
		config.DeniedPageWalk = walk
		svc, err := NewResourceSearch(searcher, checker, mock.NewMockResourceFilter(), config)
		require.NoError(t, err)
		return svc.(*ResourceSearch), searcher
	}

	t.Run("denied match is indistinguishable from no match", func(t *testing.T) {
		denied, deniedSearcher := newService(t, map[string]*model.SearchResult{
			"":   {Resources: []model.Resource{privateOrg("acme-hidden")}, PageToken: tokenPtr("p2")},
			"p2": {Resources: []model.Resource{}},
		}, []string{"acme-hidden"}, constants.DefaultDeniedPageWalk)
		absent, absentSearcher := newService(t, map[string]*model.SearchResult{}, nil, constants.DefaultDeniedPageWalk)

		deniedResult, err := denied.QueryResources(ctx, slugTag)
		require.NoError(t, err)
		absentResult, err := absent.QueryResources(ctx, slugTag)
		require.NoError(t, err)

		assert.Empty(t, deniedResult.Resources)
		assert.Nil(t, deniedResult.PageToken, "a fully denied result set must not hand back a continuation token")
		assert.Equal(t, absentResult.Resources, deniedResult.Resources)
		assert.Equal(t, absentResult.PageToken, deniedResult.PageToken)
		assert.Equal(t, []string{"", "p2"}, deniedSearcher.calls, "the walk follows the raw token to exhaustion")
		assert.Equal(t, []string{""}, absentSearcher.calls)
	})

	t.Run("walk stops at the first page with a visible resource and returns its token", func(t *testing.T) {
		svc, searcher := newService(t, map[string]*model.SearchResult{
			"":   {Resources: []model.Resource{privateOrg("hidden-1")}, PageToken: tokenPtr("p2")},
			"p2": {Resources: []model.Resource{privateOrg("hidden-2")}, PageToken: tokenPtr("p3")},
			"p3": {Resources: []model.Resource{privateOrg("visible")}, PageToken: tokenPtr("p4")},
		}, []string{"hidden-1", "hidden-2"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(ctx, slugTag)
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "visible", result.Resources[0].ID)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "p4", *result.PageToken, "continuation resumes after the page that produced the visible resource")
		assert.Equal(t, []string{"", "p2", "p3"}, searcher.calls)
	})

	t.Run("walk limit keeps the continuation token so a caller is never stranded", func(t *testing.T) {
		svc, searcher := newService(t, map[string]*model.SearchResult{
			"":   {Resources: []model.Resource{privateOrg("hidden-1")}, PageToken: tokenPtr("p2")},
			"p2": {Resources: []model.Resource{privateOrg("hidden-2")}, PageToken: tokenPtr("p3")},
			"p3": {Resources: []model.Resource{privateOrg("visible")}},
		}, []string{"hidden-1", "hidden-2"}, 1)

		result, err := svc.QueryResources(ctx, slugTag)
		require.NoError(t, err)

		assert.Empty(t, result.Resources)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "p3", *result.PageToken, "after the bounded walk the caller can continue from where the walk stopped")
		assert.Equal(t, []string{"", "p2"}, searcher.calls, "limit 1 = one extra page beyond the first")
	})

	t.Run("a visible first page is untouched by the walk", func(t *testing.T) {
		svc, searcher := newService(t, map[string]*model.SearchResult{
			"": {Resources: []model.Resource{privateOrg("acme"), privateOrg("hidden")}, PageToken: tokenPtr("p2")},
		}, []string{"hidden"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(ctx, model.SearchCriteria{Tags: []string{"slug:acme"}, PageSize: 2})
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "acme", result.Resources[0].ID)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "p2", *result.PageToken)
		assert.Equal(t, []string{""}, searcher.calls)
	})
}

func TestConfigValidate_DeniedPageWalk(t *testing.T) {
	base := DefaultConfig()

	over := base
	over.DeniedPageWalk = constants.MaxDeniedPageWalk + 1
	assert.Error(t, over.Validate())

	zero := Config{}.withDefaults()
	assert.Equal(t, constants.DefaultDeniedPageWalk, zero.DeniedPageWalk, "zero value falls back to the default")

	off := base
	off.DeniedPageWalk = 0
	assert.Error(t, off.Validate(), "the walk cannot be disabled — it is what closes the existence oracle")
}
