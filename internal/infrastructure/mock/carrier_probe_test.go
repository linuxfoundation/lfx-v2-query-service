// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockResourceSearcherTypeCarries(t *testing.T) {
	searcher := NewMockResourceSearcher()
	probe := model.CarrierProbe{Kind: model.DataField, Name: "created_at"}
	carried, err := searcher.TypeCarries(context.Background(), "project", probe)
	require.NoError(t, err)
	assert.False(t, carried)
	assert.Empty(t, searcher.CarrierProbeCalls())

	failure := errors.New("probe unavailable")
	searcher.SetTypeCarries("project", probe, true, nil)
	searcher.SetTypeCarries("committee", probe, false, failure)
	for _, tc := range []struct {
		resourceType string
		carried      bool
		err          error
	}{
		{"project", true, nil}, {"committee", false, failure}, {"meeting", false, nil},
	} {
		t.Run(tc.resourceType, func(t *testing.T) {
			got, err := searcher.TypeCarries(context.Background(), tc.resourceType, probe)
			assert.Equal(t, tc.carried, got)
			assert.ErrorIs(t, err, tc.err)
		})
	}
	calls := searcher.CarrierProbeCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, CarrierProbeCall{ResourceType: "project", Probe: probe}, calls[0])
	calls[0].ResourceType = "changed"
	assert.Equal(t, "project", searcher.CarrierProbeCalls()[0].ResourceType)
}
