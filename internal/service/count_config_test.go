// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateAccessPageBound(t *testing.T) {
	for _, tc := range []struct {
		name      string
		page, cap int
		errorText string
	}{
		{"defaults use fifty pages", 100, 5000, ""},
		{"one bucket per page cannot walk ten thousand pages", 1, 10000, "max access buckets / access bucket page must not exceed 100 pages, got 10000"},
		{"large pages use ten requests", 1000, 10000, ""},
		{"small fixture uses exactly one hundred pages", 2, 200, ""},
		{"partial final page is included in ceiling", 2, 201, "max access buckets / access bucket page must not exceed 100 pages, got 101"},
		{"non power of two exact limit", 3, 300, ""},
		{"non power of two ceiling", 3, 301, "max access buckets / access bucket page must not exceed 100 pages, got 101"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := DefaultConfig()
			config.AccessBucketPage = tc.page
			config.MaxAccessBuckets = tc.cap
			err := config.Validate()
			if tc.errorText == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.errorText)
			}
		})
	}
}
