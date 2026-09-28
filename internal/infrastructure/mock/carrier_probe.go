// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"context"
	"slices"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// CarrierProbeCall records the type and dimension requested by a presence probe.
type CarrierProbeCall struct {
	ResourceType string
	Probe        model.CarrierProbe
}

type carrierAnswer struct {
	carried bool
	err     error
}

// TypeCarries returns a programmed answer. Without programming, the mock
// treats the type as empty and retains no calls, including in a running server.
func (m *MockResourceSearcher) TypeCarries(_ context.Context, resourceType string, probe model.CarrierProbe) (bool, error) {
	m.carrierMu.Lock()
	defer m.carrierMu.Unlock()
	if m.carrierAnswers == nil {
		return false, nil
	}
	call := CarrierProbeCall{ResourceType: resourceType, Probe: probe}
	m.carrierCalls = append(m.carrierCalls, call)
	answer := m.carrierAnswers[call]
	return answer.carried, answer.err
}

// SetTypeCarries programs a probe answer and enables call recording.
func (m *MockResourceSearcher) SetTypeCarries(resourceType string, probe model.CarrierProbe, carried bool, err error) {
	m.carrierMu.Lock()
	defer m.carrierMu.Unlock()
	if m.carrierAnswers == nil {
		m.carrierAnswers = make(map[CarrierProbeCall]carrierAnswer)
	}
	m.carrierAnswers[CarrierProbeCall{ResourceType: resourceType, Probe: probe}] = carrierAnswer{carried: carried, err: err}
}

// CarrierProbeCalls returns a snapshot of calls since programming began.
func (m *MockResourceSearcher) CarrierProbeCalls() []CarrierProbeCall {
	m.carrierMu.Lock()
	defer m.carrierMu.Unlock()
	return slices.Clone(m.carrierCalls)
}
