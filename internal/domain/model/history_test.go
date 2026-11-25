// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHistoryActorPrincipal(t *testing.T) {
	for _, tt := range []struct {
		ref, kind, principal string
		valid                bool
	}{
		{"user:testuser01", "user", "testuser01", true},
		{"service:fixture-client", "service", "fixture-client", true},
		{"legacy:v1-platform/actor-001", "legacy", "v1-platform/actor-001", true},
		{"legacy:v1-salesforce/actor-001", "legacy", "v1-salesforce/actor-001", true},
		{"legacy:v1-platform/" + strings.Repeat("a", 244), "legacy", "v1-platform/" + strings.Repeat("a", 244), true},
		{"legacy:v1-platform/" + strings.Repeat("a", 245), "", "", false},
		{"legacy:actor-001", "", "", false}, {"legacy:/actor-001", "", "", false},
		{"legacy:V1/actor-001", "", "", false}, {"legacy:v1-platform/", "", "", false},
		{"legacy:v1-platform/first.last@example.com", "", "", false},
		{"user:" + strings.Repeat("a", 256), "user", strings.Repeat("a", 256), true},
		{"user:", "", "", false}, {"user:" + strings.Repeat("a", 257), "", "", false},
		{"project:fixture", "", "", false}, {"testuser01", "", "", false},
		{"user:fixture@clients", "", "", false}, {"user:fixture:other", "", "", false},
		{"user:fixture\nother", "", "", false}, {"user:fixture#other", "", "", false},
		{"user:fixture user", "", "", false}, {"user:é", "", "", false},
	} {
		kind, principal, valid := HistoryActorPrincipal(tt.ref)
		require.Equal(t, tt.valid, valid)
		require.Equal(t, tt.kind, kind)
		require.Equal(t, tt.principal, principal)
	}
}
