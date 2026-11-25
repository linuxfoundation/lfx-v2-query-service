// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"
	"regexp"
	"strings"
)

// HistoryCriteria contains only the indexed, approved history search dimensions.
type HistoryCriteria struct {
	SearchCriteria
}

// HistoryProfile combines live display fields with retained event identity.
// Lookup failure never changes the retained email or username.
type HistoryProfile struct {
	Availability string  `json:"availability"`
	Name         *string `json:"name"`
	AvatarURL    *string `json:"avatar_url"`
	Email        *string `json:"email"`
	Username     *string `json:"username"`
}

// HistoryAttribution records initiating and effective actors at mutation time.
type HistoryAttribution struct {
	ActorRef               string          `json:"actor_ref"`
	EffectiveActorRef      string          `json:"effective_actor_ref"`
	ActorProfile           *HistoryProfile `json:"actor_profile,omitempty"`
	EffectiveActorProfile  *HistoryProfile `json:"effective_actor_profile,omitempty"`
	ActorVerified          bool            `json:"actor_verified"`
	EffectiveActorVerified bool            `json:"effective_actor_verified"`
	// Retained identities are storage input, not additional public API fields.
	ActorIdentity          *HistoryIdentity `json:"-"`
	EffectiveActorIdentity *HistoryIdentity `json:"-"`
}

// HistoryIdentity is the indexer-owned data-governance record, never refreshed.
type HistoryIdentity struct {
	Username *string `json:"username"`
	Email    *string `json:"email"`
}

// HistoryDisplayProfile contains only live directory display data. Found=false
// means an explicit user-not-found result; errors must not be cached as deletion.
type HistoryDisplayProfile struct {
	Found     bool
	Name      *string
	AvatarURL *string
}

var historyLegacySourcePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// HistoryActorPrincipal parses user/service principals and source-qualified
// legacy provenance. Legacy IDs are not directory keys. This checks syntax
// only, not origin, verification or history access.
func HistoryActorPrincipal(ref string) (kind, principal string, valid bool) {
	kind, principal, ok := strings.Cut(ref, ":")
	if !ok || (kind != "user" && kind != "service" && kind != "legacy") || len(principal) == 0 || len(principal) > 256 {
		return "", "", false
	}
	for _, r := range principal {
		if r < 0x21 || r > 0x7e || strings.ContainsRune(":#@{}", r) {
			return "", "", false
		}
	}
	if kind == "legacy" {
		source, sourceID, ok := strings.Cut(principal, "/")
		if !ok || !historyLegacySourcePattern.MatchString(source) || sourceID == "" {
			return "", "", false
		}
	}
	return kind, principal, true
}

// HistoryContent contains only approved publisher snapshot fields.
type HistoryContent struct {
	Schema         string          `json:"schema"`
	Representation string          `json:"representation"`
	Before         json.RawMessage `json:"before,omitempty"`
	After          json.RawMessage `json:"after,omitempty"`
}

// HistoryEvent is the public projection plus its private authorization key.
type HistoryEvent struct {
	Publisher            string
	EventID              string
	ObjectType           string
	ObjectID             string
	Action               string
	OccurredAt           string
	ResourceRevision     uint64
	Attribution          HistoryAttribution
	Content              HistoryContent
	HistoryCheckObject   string
	HistoryCheckRelation string
}

// HistoryPage is one raw index page, before per-event access filtering.
type HistoryPage struct {
	Events      []HistoryEvent
	SearchAfter *string
}

// HistoryResult is a caller-visible page plus the raw continuation cursor.
type HistoryResult struct {
	Events      []HistoryEvent
	SearchAfter *string
}
