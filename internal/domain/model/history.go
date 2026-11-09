// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

import "encoding/json"

// HistoryCriteria contains only the indexed, approved history search dimensions.
type HistoryCriteria struct {
	SearchCriteria
}

// HistoryProfile is a source-captured user profile, never a live lookup.
type HistoryProfile struct {
	Availability string  `json:"availability"`
	Name         *string `json:"name"`
	AvatarURL    *string `json:"avatar_url"`
	Email        *string `json:"email"`
	Username     *string `json:"username"`
}

// HistoryAttribution records initiating and effective actors at mutation time.
type HistoryAttribution struct {
	ActorRef              string          `json:"actor_ref"`
	EffectiveActorRef     string          `json:"effective_actor_ref"`
	ActorProfile          *HistoryProfile `json:"actor_profile,omitempty"`
	EffectiveActorProfile *HistoryProfile `json:"effective_actor_profile,omitempty"`
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
