// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedSearcher scripts raw OpenSearch pages keyed by the incoming
// search_after cursor — the field the real adapter renders into the query —
// with the first page keyed by "". It deliberately ignores PageToken, exactly
// like production, so a walk that only advances the opaque token cannot pass.
// Each scripted page carries the cursor the adapter would hand back next to
// its token. It embeds the mock only to satisfy the rest of the port.
type pagedSearcher struct {
	*mock.MockResourceSearcher
	pages   map[string]*model.SearchResult
	errAt   map[string]error
	cursors []string
}

func (p *pagedSearcher) QueryResources(_ context.Context, criteria model.SearchCriteria) (*model.SearchResult, error) {
	key := ""
	if criteria.SearchAfter != nil {
		key = *criteria.SearchAfter
	}
	p.cursors = append(p.cursors, key)
	if err, ok := p.errAt[key]; ok {
		return nil, err
	}
	page, ok := p.pages[key]
	if !ok {
		return &model.SearchResult{Resources: []model.Resource{}}, nil
	}
	// Copy so the service's in-place filtering cannot leak between calls.
	out := &model.SearchResult{PageToken: page.PageToken, NextSearchAfter: page.NextSearchAfter}
	out.Resources = append(out.Resources, page.Resources...)
	return out, nil
}

// page builds a raw page; a non-empty next cursor means the raw page was full
// and the adapter minted a token for it.
func page(next string, resources ...model.Resource) *model.SearchResult {
	out := &model.SearchResult{Resources: resources}
	if next != "" {
		token := "tok-" + next
		out.PageToken = &token
		out.NextSearchAfter = &next
	}
	return out
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

func publicOrg(id string) model.Resource {
	org := privateOrg(id)
	org.Public = true
	return org
}

func strPtr(s string) *string { return &s }

// idContainsFilter stands in for the CEL filter: it keeps resources whose ID
// contains the expression, so a scripted raw page can be emptied by filtering
// rather than by the access check.
type idContainsFilter struct{}

func (idContainsFilter) Filter(_ context.Context, resources []model.Resource, expression string) ([]model.Resource, error) {
	kept := make([]model.Resource, 0, len(resources))
	for _, resource := range resources {
		if strings.Contains(resource.ID, expression) {
			kept = append(kept, resource)
		}
	}
	return kept, nil
}

// TestResourceSearchQueryResources_DeniedPagesDoNotLeakExistence pins the
// contract that an authenticated caller cannot tell "matches exist but you may
// not see them" from "nothing matches": both come back as an empty page with
// no page_token. Before the denied-page walk, the former returned a token
// (minted from the raw hits before the access check) and the latter did not —
// an existence oracle for any exact-tag lookup such as an organization slug.
func TestResourceSearchQueryResources_DeniedPagesDoNotLeakExistence(t *testing.T) {
	viewer := context.WithValue(context.Background(), constants.PrincipalContextID, "viewer")
	slugTag := model.SearchCriteria{Tags: []string{"slug:acme-corp"}, PageSize: 1}

	newService := func(t *testing.T, pages map[string]*model.SearchResult, denied []string, walk int) (*ResourceSearch, *pagedSearcher, *mock.MockAccessControlChecker) {
		t.Helper()
		searcher := &pagedSearcher{MockResourceSearcher: mock.NewMockResourceSearcher(), pages: pages, errAt: map[string]error{}}
		checker := mock.NewMockAccessControlChecker()
		checker.DefaultResult = "allowed"
		checker.DeniedResourceIDs = denied
		config := DefaultConfig()
		config.DeniedPageWalk = walk
		svc, err := NewResourceSearch(searcher, checker, idContainsFilter{}, config)
		require.NoError(t, err)
		return svc.(*ResourceSearch), searcher, checker
	}

	t.Run("denied match is indistinguishable from no match", func(t *testing.T) {
		denied, deniedSearcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("acme-hidden")),
			"c2": page(""),
		}, []string{"acme-hidden"}, constants.DefaultDeniedPageWalk)
		absent, absentSearcher, _ := newService(t, map[string]*model.SearchResult{}, nil, constants.DefaultDeniedPageWalk)

		deniedResult, err := denied.QueryResources(viewer, slugTag)
		require.NoError(t, err)
		absentResult, err := absent.QueryResources(viewer, slugTag)
		require.NoError(t, err)

		assert.Empty(t, deniedResult.Resources)
		assert.Nil(t, deniedResult.PageToken, "a fully denied result set must not hand back a continuation token")
		assert.Equal(t, absentResult.Resources, deniedResult.Resources)
		assert.Equal(t, absentResult.PageToken, deniedResult.PageToken)
		assert.Equal(t, []string{"", "c2"}, deniedSearcher.cursors, "the walk follows the raw search_after cursor to exhaustion")
		assert.Equal(t, []string{""}, absentSearcher.cursors)
	})

	t.Run("walk advances the search_after cursor, not the opaque token", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("hidden-1")),
			"c2": page("c3", privateOrg("hidden-2")),
			"c3": page("c4", privateOrg("visible")),
		}, []string{"hidden-1", "hidden-2"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(viewer, slugTag)
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "visible", result.Resources[0].ID)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "tok-c4", *result.PageToken, "continuation resumes after the page that produced the visible resource")
		assert.Equal(t, []string{"", "c2", "c3"}, searcher.cursors, "each iteration queries with the previous page's cursor")
	})

	t.Run("a caller resuming from a token continues from its cursor", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("seen-already")),
			"c2": page("c3", privateOrg("hidden")),
			"c3": page("", privateOrg("visible")),
		}, []string{"hidden"}, constants.DefaultDeniedPageWalk)

		// The HTTP layer decodes the incoming page_token into SearchAfter.
		resume := slugTag
		resume.PageToken = strPtr("tok-c2")
		resume.SearchAfter = strPtr("c2")

		result, err := svc.QueryResources(viewer, resume)
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "visible", result.Resources[0].ID)
		assert.Nil(t, result.PageToken)
		assert.Equal(t, []string{"c2", "c3"}, searcher.cursors, "never restarts from the first page")
	})

	t.Run("walk limit keeps the continuation token so a caller is never stranded", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("hidden-1")),
			"c2": page("c3", privateOrg("hidden-2")),
			"c3": page("", privateOrg("visible")),
		}, []string{"hidden-1", "hidden-2"}, 1)

		result, err := svc.QueryResources(viewer, slugTag)
		require.NoError(t, err)

		assert.Empty(t, result.Resources)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "tok-c3", *result.PageToken, "after the bounded walk the caller can continue from where the walk stopped")
		assert.Equal(t, []string{"", "c2"}, searcher.cursors, "limit 1 = one extra page beyond the first")
	})

	t.Run("a visible first page is untouched by the walk", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"": page("c2", privateOrg("acme"), privateOrg("hidden")),
		}, []string{"hidden"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(viewer, model.SearchCriteria{Tags: []string{"slug:acme"}, PageSize: 2})
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "acme", result.Resources[0].ID)
		require.NotNil(t, result.PageToken)
		assert.Equal(t, "tok-c2", *result.PageToken)
		assert.Equal(t, []string{""}, searcher.cursors)
	})

	t.Run("a page emptied by the CEL filter is walked the same way", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", publicOrg("acme")),
			"c2": page("", publicOrg("acme-labs")),
		}, nil, constants.DefaultDeniedPageWalk)

		// idContainsFilter keeps resources whose ID contains the expression.
		criteria := model.SearchCriteria{Tags: []string{"slug:acme"}, PageSize: 1, CelFilter: strPtr("labs")}
		result, err := svc.QueryResources(viewer, criteria)
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "acme-labs", result.Resources[0].ID)
		assert.Nil(t, result.PageToken)
		assert.Equal(t, []string{"", "c2"}, searcher.cursors)
	})

	t.Run("anonymous callers keep the cache-control header after a walk", func(t *testing.T) {
		// Anonymous searches are PublicOnly at the adapter, so their pages only
		// empty out through the CEL filter; the walk must still end with the
		// anonymous cache-control header set.
		anon := context.WithValue(context.Background(), constants.PrincipalContextID, constants.AnonymousPrincipal)
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", publicOrg("acme")),
			"c2": page("", publicOrg("acme-labs")),
		}, nil, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(anon, model.SearchCriteria{Tags: []string{"slug:acme"}, PageSize: 1, CelFilter: strPtr("labs")})
		require.NoError(t, err)

		require.Len(t, result.Resources, 1)
		assert.Equal(t, "acme-labs", result.Resources[0].ID)
		require.NotNil(t, result.CacheControl)
		assert.Equal(t, constants.AnonymousCacheControlHeader, *result.CacheControl)
		assert.Equal(t, []string{"", "c2"}, searcher.cursors)
	})

	t.Run("a search error on a later page aborts the walk", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"": page("c2", privateOrg("hidden")),
		}, []string{"hidden"}, constants.DefaultDeniedPageWalk)
		searcher.errAt["c2"] = errors.New("opensearch unavailable")

		result, err := svc.QueryResources(viewer, slugTag)
		require.Error(t, err)
		assert.Nil(t, result, "no empty-page-with-token consolation prize on error")
		assert.Equal(t, []string{"", "c2"}, searcher.cursors)
	})

	t.Run("an access check error on a later page aborts the walk", func(t *testing.T) {
		svc, _, checker := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("hidden-1")),
			"c2": page("", privateOrg("hidden-2")),
		}, []string{"hidden-1"}, constants.DefaultDeniedPageWalk)
		checker.SetCheckAccessErrorOnCall(2, errors.New("fga-sync timeout"))

		result, err := svc.QueryResources(viewer, slugTag)
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("a cancelled context stops the walk", func(t *testing.T) {
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{
			"":   page("c2", privateOrg("hidden-1")),
			"c2": page("c3", privateOrg("hidden-2")),
		}, []string{"hidden-1", "hidden-2"}, constants.DefaultDeniedPageWalk)
		cancelled, cancel := context.WithCancel(viewer)
		cancel()

		result, err := svc.QueryResources(cancelled, slugTag)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, []string{""}, searcher.cursors, "no further page is fetched once the context is done")
	})

	t.Run("a cursor that does not advance is an adapter defect, not a refetch loop", func(t *testing.T) {
		stuck := page("c2", privateOrg("hidden"))
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{"": stuck, "c2": stuck}, []string{"hidden"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(viewer, slugTag)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, []string{"", "c2"}, searcher.cursors, "stops as soon as the cursor repeats instead of walking to the limit")
	})

	t.Run("a token without its cursor is an adapter defect, not an infinite loop", func(t *testing.T) {
		broken := page("c2", privateOrg("hidden"))
		broken.NextSearchAfter = nil
		svc, searcher, _ := newService(t, map[string]*model.SearchResult{"": broken}, []string{"hidden"}, constants.DefaultDeniedPageWalk)

		result, err := svc.QueryResources(viewer, slugTag)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, []string{""}, searcher.cursors)
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
