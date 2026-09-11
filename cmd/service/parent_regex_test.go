// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"regexp"
	"testing"
)

// parentPattern mirrors the Goa design pattern at design/query-svc.go.
// This test exists to guard against accidental regex regression.
const parentPattern = `^[a-zA-Z][a-zA-Z0-9_]*:[a-zA-Z0-9_-]+$`

func TestParentPattern(t *testing.T) {
	re := regexp.MustCompile(parentPattern)

	valid := []string{
		"project:123",
		"past_meeting:98471391296-1765832400000",
		"v1_meeting:abc-123",
		"v1_past_meeting:foo_bar-baz",
		"committee:abc",
	}
	for _, v := range valid {
		if !re.MatchString(v) {
			t.Errorf("expected %q to match parent pattern", v)
		}
	}

	invalid := []string{
		"past_meeting:",    // empty id fragment
		":abc",             // empty type fragment
		"_leading:abc",     // leading underscore in type
		"past meeting:abc", // space in type
		"project",          // missing colon
		"",                 // empty
	}
	for _, v := range invalid {
		if re.MatchString(v) {
			t.Errorf("expected %q to NOT match parent pattern", v)
		}
	}
}
