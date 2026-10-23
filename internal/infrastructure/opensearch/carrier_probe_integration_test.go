// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

//go:build integration

package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds the opt-in integration test of the carrier probe against a
// real OpenSearch engine and the real resources index mapping. It is excluded
// from the ordinary gate by its build tag and skips itself without an engine:
//
//	OPENSEARCH_TEST_URL=http://127.0.0.1:9200 \
//	  go test -tags integration -run TestOpenSearchSearcherTypeCarriesIntegration \
//	  -v ./internal/infrastructure/opensearch/
//
// Run it against every engine version the service must support. The unit
// tests in carrier_probe_test.go check what the probe sends; this test checks
// what the engine answers, which for a flat_object field depends on the
// engine version (see the data-field assertions below).

// carrierProbeIndexMapping is the indexer's index template, copied verbatim;
// the test substitutes the two Helm values it carries.
const carrierProbeIndexMapping = "testdata/opensearch-resources-index.json.tpl"

func TestOpenSearchSearcherTypeCarriesIntegration(t *testing.T) {
	url := os.Getenv("OPENSEARCH_TEST_URL")
	if url == "" {
		t.Skip("OPENSEARCH_TEST_URL is not set; skipping the OpenSearch integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	engine := newIntegrationEngine(t, ctx, url)
	t.Logf("engine: %s", engine.version())

	index := fmt.Sprintf("carrier-probe-%d", time.Now().UnixNano())
	engine.createIndex(index)
	t.Cleanup(func() { engine.deleteIndex(index) })

	// Two types share the final name "name" on different paths: committee
	// members carry it only nested (data.project.name, data.role.name) and
	// committees only at the top level (data.name). Every other field is
	// distinct, so each assertion isolates one question.
	const memberType, committeeType = "committee_member", "committee"
	engine.index(index, "committee_member:cm-1", map[string]any{
		"object_type": memberType, "object_id": "cm-1", "object_ref": memberType + ":cm-1",
		"parent_refs": []string{"committee:c-1"}, "tags": []string{"committee_uid:c-1"},
		"public": true, "latest": true,
		"data": map[string]any{
			"uid":      "cm-1",
			"role":     map[string]any{"name": "Chair"},
			"project":  map[string]any{"name": "Example Project", "id": "p-1"},
			"nullable": nil,
		},
	})
	engine.index(index, "committee_member:cm-2", map[string]any{
		"object_type": memberType, "object_id": "cm-2", "object_ref": memberType + ":cm-2",
		"parent_refs": []string{"committee:c-1"}, "tags": []string{"committee_uid:c-1"},
		"public": true, "latest": true,
		"data": map[string]any{
			"uid":  "cm-2",
			"role": map[string]any{"name": "Member"},
		},
	})
	engine.index(index, "committee:c-1", map[string]any{
		"object_type": committeeType, "object_id": "c-1", "object_ref": committeeType + ":c-1",
		"parent_refs": []string{"project:p-1"}, "tags": []string{"project_uid:p-1"},
		"public": true, "latest": true,
		"data": map[string]any{
			"uid":  "c-1",
			"name": "Example Committee",
		},
	})
	engine.refresh(index)

	searcher, err := NewSearcher(ctx, Config{URL: url, Index: index})
	require.NoError(t, err)

	for _, tc := range []struct {
		name         string
		resourceType string
		probe        model.CarrierProbe
		want         bool
	}{
		// Document presence, parents and tags: engine-version independent.
		{"any document of an indexed type", memberType, model.CarrierProbe{Kind: model.AnyDocument}, true},
		{"any document of an unknown type", "no_such_type", model.CarrierProbe{Kind: model.AnyDocument}, false},
		{"parent kind carried", memberType, model.CarrierProbe{Kind: model.ParentKind, Name: "committee"}, true},
		{"parent kind absent", memberType, model.CarrierProbe{Kind: model.ParentKind, Name: "project"}, false},
		{"tag prefix carried", memberType, model.CarrierProbe{Kind: model.TagPrefix, Name: "committee_uid"}, true},
		{"tag prefix absent", memberType, model.CarrierProbe{Kind: model.TagPrefix, Name: "project_uid"}, false},

		// Data fields on the flat_object mapping. The top-level and absent
		// cases hold on every engine; the nested and shared-name cases
		// hold only where the engine resolves a dotted flat_object path
		// (OpenSearch 3.0 and newer with an exists probe).
		{"top-level field carried", memberType, model.CarrierProbe{Kind: model.DataField, Name: "uid"}, true},
		{"absent field", memberType, model.CarrierProbe{Kind: model.DataField, Name: "missing"}, false},
		{"nested field carried", memberType, model.CarrierProbe{Kind: model.DataField, Name: "role.name"}, true},
		{"nested field carried on one document only", memberType, model.CarrierProbe{Kind: model.DataField, Name: "project.id"}, true},
		{"top-level field carried by the other type", committeeType, model.CarrierProbe{Kind: model.DataField, Name: "name"}, true},
		{"nested-only final name does not carry the top-level field", memberType, model.CarrierProbe{Kind: model.DataField, Name: "name"}, false},
		{"top-level-only final name does not carry the nested field", committeeType, model.CarrierProbe{Kind: model.DataField, Name: "project.name"}, false},
		{"a field of another type is not carried", committeeType, model.CarrierProbe{Kind: model.DataField, Name: "role.name"}, false},

		// Edge cases the contract keeps: a null-only field and an
		// object-valued field are not carried.
		{"null-only field is not carried", memberType, model.CarrierProbe{Kind: model.DataField, Name: "nullable"}, false},
		{"object-valued field is not carried", memberType, model.CarrierProbe{Kind: model.DataField, Name: "role"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			carried, err := searcher.TypeCarries(ctx, tc.resourceType, tc.probe)
			require.NoError(t, err)
			assert.Equal(t, tc.want, carried, "%s probe %q on type %s", tc.probe.Kind, tc.probe.Name, tc.resourceType)
		})
	}
}

// integrationEngine is the test's own minimal client for index setup: the
// searcher under test only reads, so index creation, indexing and refresh go
// straight to the engine's REST API.
type integrationEngine struct {
	t      *testing.T
	ctx    context.Context
	url    string
	client *http.Client
}

func newIntegrationEngine(t *testing.T, ctx context.Context, url string) *integrationEngine {
	t.Helper()
	return &integrationEngine{t: t, ctx: ctx, url: strings.TrimRight(url, "/"), client: &http.Client{Timeout: 30 * time.Second}}
}

// do sends one request and returns the body; any non-2xx status fails the test.
func (e *integrationEngine) do(method, path string, body []byte) []byte {
	e.t.Helper()
	req, err := http.NewRequestWithContext(e.ctx, method, e.url+path, bytes.NewReader(body))
	require.NoError(e.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client.Do(req)
	require.NoError(e.t, err, "%s %s", method, path)
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	require.Less(e.t, resp.StatusCode, 300, "%s %s: %s", method, path, out)
	return out
}

func (e *integrationEngine) version() string {
	var info struct {
		Version struct {
			Distribution string `json:"distribution"`
			Number       string `json:"number"`
		} `json:"version"`
	}
	require.NoError(e.t, json.Unmarshal(e.do(http.MethodGet, "/", nil), &info))
	return info.Version.Distribution + " " + info.Version.Number
}

// createIndex renders the indexer's template for a single-shard, unreplicated
// throwaway index and creates it.
func (e *integrationEngine) createIndex(index string) {
	e.t.Helper()
	tpl, err := template.ParseFiles(carrierProbeIndexMapping)
	require.NoError(e.t, err)
	var body bytes.Buffer
	require.NoError(e.t, tpl.Execute(&body, map[string]any{
		"Values": map[string]any{"opensearch": map[string]any{"indexingJob": map[string]any{
			"numberOfShards": 1, "numberOfReplicas": 0,
		}}},
	}))
	require.True(e.t, json.Valid(body.Bytes()), "rendered mapping is not valid JSON:\n%s", body.String())
	e.do(http.MethodPut, "/"+index, body.Bytes())
}

func (e *integrationEngine) deleteIndex(index string) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, e.url+"/"+index, nil)
	if err != nil {
		return
	}
	resp, err := e.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (e *integrationEngine) index(index, id string, document map[string]any) {
	e.t.Helper()
	body, err := json.Marshal(document)
	require.NoError(e.t, err)
	e.do(http.MethodPut, "/"+index+"/_doc/"+id, body)
}

func (e *integrationEngine) refresh(index string) {
	e.t.Helper()
	e.do(http.MethodPost, "/"+index+"/_refresh", nil)
}
