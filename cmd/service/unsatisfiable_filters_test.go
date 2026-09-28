// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"os"
	"os/exec"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceSearchConfigImplUnsatisfiableFilters(t *testing.T) {
	for _, tc := range []struct {
		value    string
		disabled bool
	}{{"", false}, {"true", false}, {"false", true}} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("UNSATISFIABLE_FILTER_REJECTION", tc.value)
			config := ResourceSearchConfigImpl(context.Background())
			assert.Equal(t, tc.disabled, config.DisableUnsatisfiableFilterRejection)
		})
	}
	t.Run("invalid value fails startup", func(t *testing.T) {
		if os.Getenv("TEST_INVALID_FILTER_TOGGLE") == "1" {
			ResourceSearchConfigImpl(context.Background())
			return
		}
		t.Setenv("TEST_INVALID_FILTER_TOGGLE", "1")
		t.Setenv("UNSATISFIABLE_FILTER_REJECTION", "invalid")
		cmd := exec.Command(os.Args[0], "-test.run=^TestResourceSearchConfigImplUnsatisfiableFilters$/invalid_value_fails_startup$")
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(output), "invalid UNSATISFIABLE_FILTER_REJECTION")
	})
}

func TestUnsatisfiableFiltersAPIErrorMapping(t *testing.T) {
	for _, route := range []string{"search", "count"} {
		for _, outage := range []bool{false, true} {
			t.Run(route+map[bool]string{false: "/absent", true: "/outage"}[outage], func(t *testing.T) {
				searcher := mock.NewMockResourceSearcher()
				searcher.SetQueryResourcePages(&model.SearchResult{})
				searcher.SetCountPublicResponse(0)
				searcher.SetAccessBucketPages(&model.AccessBucketPage{})
				searcher.SetTypeCarries("committee", model.CarrierProbe{Kind: model.AnyDocument}, true, nil)
				if outage {
					searcher.SetTypeCarries("committee", model.CarrierProbe{Kind: model.DataField, Name: "start_time"}, false, errors.NewValidation("upstream rejected probe"))
				}
				svc := newTestQuerySvc(t, searcher, mock.NewMockAccessControlChecker(), mock.NewMockOrganizationSearcher(), mock.NewMockAuthService())
				ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "caller")
				var err error
				if route == "search" {
					_, err = svc.QueryResources(ctx, &querysvc.QueryResourcesPayload{Version: "1", Type: stringPtr("committee"), DateField: stringPtr("start_time"), DateFrom: stringPtr("2025-01-01"), Sort: "name_asc", PageSize: 50})
				} else {
					_, err = svc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{Version: "1", Type: stringPtr("committee"), DateField: stringPtr("start_time"), DateFrom: stringPtr("2025-01-01")})
				}
				if outage {
					var unavailable *querysvc.ServiceUnavailableError
					require.ErrorAs(t, err, &unavailable)
				} else {
					var badRequest *querysvc.BadRequestError
					require.ErrorAs(t, err, &badRequest)
					assert.Equal(t, `date_field "start_time" is not carried by any indexed committee document`, badRequest.Message)
				}
			})
		}
	}
}
