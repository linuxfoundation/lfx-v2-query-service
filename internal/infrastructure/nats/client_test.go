// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckWireSize(t *testing.T) {
	tests := []struct {
		name       string
		subject    string
		size       int64
		maxPayload int64
		wantErr    bool
	}{
		{
			name:       "under limit",
			subject:    "access.check",
			size:       1024,
			maxPayload: 1024 * 1024,
			wantErr:    false,
		},
		{
			name:       "exactly at limit",
			subject:    "access.check",
			size:       1024 * 1024,
			maxPayload: 1024 * 1024,
			wantErr:    false,
		},
		{
			name:       "over limit, e.g. caller-supplied baggage inflated the injected headers past the negotiated max payload even though the request stayed under the fixed chunking margin",
			subject:    "access.check",
			size:       1024*1024 + 1,
			maxPayload: 1024 * 1024,
			wantErr:    true,
		},
		{
			name:       "unset max payload (no INFO handshake yet) never rejects",
			subject:    "access.check",
			size:       10 * 1024 * 1024,
			maxPayload: 0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkWireSize(tt.subject, tt.size, tt.maxPayload)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.subject)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
