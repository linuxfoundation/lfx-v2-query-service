// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/mock"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// HistoryProfileReader supplies explicit directory success/absence/failure.
type HistoryProfileReader struct{ mock.Mock }

// LookupHistoryProfile returns only live display fields configured by the test.
func (m *HistoryProfileReader) LookupHistoryProfile(ctx context.Context, principal string) (model.HistoryDisplayProfile, error) {
	args := m.Called(ctx, principal)
	return args.Get(0).(model.HistoryDisplayProfile), args.Error(1)
}

// HistoryProfileRequester is a narrow NATS request/reply transport mock.
type HistoryProfileRequester struct{ mock.Mock }

// RequestWithContext returns the configured auth-service response or failure.
func (m *HistoryProfileRequester) RequestWithContext(ctx context.Context, subject string, data []byte) (*natsgo.Msg, error) {
	args := m.Called(ctx, subject, data)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*natsgo.Msg), args.Error(1)
}
