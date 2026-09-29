// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	queryserver "github.com/linuxfoundation/lfx-v2-query-service/gen/http/query_svc/server"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestParentHTTPDecodeAndConvert(t *testing.T) {
	for _, route := range []string{"search", "count"} {
		for _, tc := range []struct {
			name    string
			query   string
			parent  *string
			invalid bool
		}{
			{name: "omitted"},
			{name: "empty", query: "&parent="},
			{name: "valid", query: "&parent=project:example", parent: stringPtr("project:example")},
			{name: "missing colon", query: "&parent=project", invalid: true},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				path := "/query/resources"
				decode := queryserver.DecodeQueryResourcesRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)
				if route == "count" {
					path += "/count"
					decode = queryserver.DecodeQueryResourcesCountRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)
				}
				req := httptest.NewRequest(http.MethodGet, path+"?v=1&type=committee"+tc.query, nil)
				// Decoding requires a header but does not authenticate its value.
				req.Header.Set("Authorization", "Bearer test-only")
				decoded, err := decode(req)
				if tc.invalid {
					require.ErrorContains(t, err, "parent")
					assert.Equal(t, http.StatusBadRequest, goahttp.NewErrorResponse(req.Context(), err).StatusCode())
					return
				}
				require.NoError(t, err)
				svc := &querySvcsrvc{}
				var criteria []model.SearchCriteria
				if route == "search" {
					payload := decoded.(*querysvc.QueryResourcesPayload)
					assert.Equal(t, tc.parent, payload.Parent, "decoded parent")
					converted, err := svc.payloadToCriteria(context.Background(), payload)
					require.NoError(t, err)
					criteria = append(criteria, converted)
				} else {
					payload := decoded.(*querysvc.QueryResourcesCountPayload)
					assert.Equal(t, tc.parent, payload.Parent, "decoded parent")
					public, err := svc.payloadToCountPublicCriteria(payload)
					require.NoError(t, err)
					private, err := svc.payloadToCountPrivateCriteria(payload)
					require.NoError(t, err)
					criteria = append(criteria, public, private)
				}
				for _, converted := range criteria {
					assert.Equal(t, tc.parent, converted.Parent, "converted parent")
					body, err := (&opensearch.OpenSearchSearcher{}).Render(context.Background(), converted)
					require.NoError(t, err)
					if tc.parent == nil {
						assert.NotContains(t, string(body), `"parent_refs"`)
					} else {
						assert.Contains(t, string(body), `"parent_refs":"project:example"`)
					}
				}
			})
		}
	}
}
