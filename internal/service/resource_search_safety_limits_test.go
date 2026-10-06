// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
)

// TestConfigValidateRequestTimeoutBounds exercises the negative/zero, minimum,
// maximum and maximum+1 boundaries for the three request-timeout fields added
// to Config.Validate.
func TestConfigValidateRequestTimeoutBounds(t *testing.T) {
	for _, field := range []struct {
		name      string
		max       time.Duration
		errorText string
		apply     func(c *Config, v time.Duration)
	}{
		{
			name:      "CountRequestTimeout",
			max:       constants.MaxCountRequestTimeout,
			errorText: "count request timeout must be between 1ns and",
			apply:     func(c *Config, v time.Duration) { c.CountRequestTimeout = v },
		},
		{
			name:      "SearchRequestTimeout",
			max:       constants.MaxSearchRequestTimeout,
			errorText: "search request timeout must be between 1ns and",
			apply:     func(c *Config, v time.Duration) { c.SearchRequestTimeout = v },
		},
		{
			name:      "SummaryRequestTimeout",
			max:       constants.MaxSummaryRequestTimeout,
			errorText: "summary request timeout must be between 1ns and",
			apply:     func(c *Config, v time.Duration) { c.SummaryRequestTimeout = v },
		},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				value     time.Duration
				wantError bool
			}{
				{"negative is rejected", -time.Second, true},
				{"zero is rejected", 0, true},
				{"minimum (1ns) is accepted", 1, false},
				{"maximum is accepted", field.max, false},
				{"maximum+1 is rejected", field.max + 1, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					config := DefaultConfig()
					field.apply(&config, tc.value)
					err := config.Validate()
					if tc.wantError {
						require.ErrorContains(t, err, field.errorText)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

// TestConfigValidateAccessCheckChunkBytesBound exercises the negative/zero,
// minimum, maximum and maximum+1 boundaries for AccessCheckChunkBytes.
func TestConfigValidateAccessCheckChunkBytesBound(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     int
		wantError bool
	}{
		{"negative is rejected", -1, true},
		{"zero is rejected", 0, true},
		{"minimum (1 byte) is accepted", 1, false},
		{"maximum is accepted", constants.MaxAccessCheckChunkBytes, false},
		{"maximum+1 is rejected", constants.MaxAccessCheckChunkBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.AccessCheckChunkBytes = tc.value
			err := config.Validate()
			if tc.wantError {
				require.ErrorContains(t, err, "access check chunk bytes must be between 1 and")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestConfigValidateAccessCheckRetriesBound exercises the negative, minimum,
// maximum and maximum+1 boundaries for AccessCheckRetries.
func TestConfigValidateAccessCheckRetriesBound(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     int
		wantError bool
	}{
		{"negative is rejected", -1, true},
		{"minimum (0 retries) is accepted", 0, false},
		{"maximum is accepted", constants.MaxAccessCheckRetries, false},
		{"maximum+1 is rejected", constants.MaxAccessCheckRetries + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.AccessCheckRetries = tc.value
			err := config.Validate()
			if tc.wantError {
				require.ErrorContains(t, err, "access check retries must be between 0 and")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
