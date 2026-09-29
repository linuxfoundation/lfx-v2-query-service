// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// Keep presence probes separate from result queries: no caller scope, access
// filter, latest filter, pagination, or record data belongs in this request.
// Bounded hit tracking is required because false omits hits.total entirely.
var carrierProbeTemplate = template.Must(template.New("carrierProbe").Funcs(templateFuncs).Parse(`{
  "size": 0,
  "terminate_after": 1,
  "track_total_hits": 1,
  "query": {"bool": {"filter": [
    {"term": {"object_type": {{quote .ResourceType}}}}
    {{if .Exists}}, {"exists": {"field": {{quote .Field}}}}
    {{else if .Field}}, {"prefix": { {{quote .Field}}: {{quote .Prefix}} }}{{end}}
  ]}}
}`))

// TypeCarries implements port.ResourceSearcher without reading any record data.
func (os *OpenSearchSearcher) TypeCarries(ctx context.Context, resourceType string, probe model.CarrierProbe) (bool, error) {
	params := struct {
		ResourceType string
		Field        string
		Prefix       string
		Exists       bool
	}{ResourceType: resourceType}
	switch probe.Kind {
	case model.AnyDocument:
	case model.DataField:
		params.Field, params.Exists = "data."+probe.Name, true
	case model.ParentKind:
		params.Field, params.Prefix = "parent_refs", probe.Name+":"
	case model.TagPrefix:
		params.Field, params.Prefix = "tags", probe.Name+":"
	default:
		return false, fmt.Errorf("unsupported carrier probe kind %q", probe.Kind)
	}
	var body bytes.Buffer
	if err := carrierProbeTemplate.Execute(&body, params); err != nil {
		return false, fmt.Errorf("failed to render carrier probe: %w", err)
	}
	response, err := os.client.Search(ctx, os.index, body.Bytes(), 0)
	if err != nil {
		return false, fmt.Errorf("opensearch carrier probe failed: %w", err)
	}
	if response == nil {
		return false, fmt.Errorf("opensearch carrier probe returned no response")
	}
	return response.Total.Value > 0, nil
}
