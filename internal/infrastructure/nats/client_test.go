// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
)

func TestWirePayloadSize(t *testing.T) {
	t.Run("no headers counts only data", func(t *testing.T) {
		msg := nats.NewMsg("subj.with.some.length")
		msg.Data = []byte("payload")
		assert.Equal(t, int64(len("payload")), wirePayloadSize(msg))
	})

	t.Run("headers counted, subject and reply excluded", func(t *testing.T) {
		msg := nats.NewMsg("a-subject-longer-than-the-data-and-headers-combined")
		msg.Reply = "a-reply-subject-also-longer-than-the-rest"
		msg.Data = []byte("x")
		withoutHeaders := wirePayloadSize(msg)

		msg.Header = nats.Header{"traceparent": []string{"00-abc-def-01"}}
		withHeaders := wirePayloadSize(msg)

		assert.Greater(t, withHeaders, withoutHeaders, "adding a header must increase the counted size")
		// Subject+Reply together are far longer than Data+headers here; if
		// wirePayloadSize counted them (as nats.Msg.Size() does), the result
		// would exceed the subject/reply lengths on its own.
		assert.Less(t, withHeaders, int64(len(msg.Subject)+len(msg.Reply)),
			"wirePayloadSize must not include Subject or Reply")
	})
}

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
