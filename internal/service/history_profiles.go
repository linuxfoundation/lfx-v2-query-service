// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"sync"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
)

type historyProfileCacheEntry struct {
	profile model.HistoryDisplayProfile
	expires time.Time
	used    uint64
}

// HistoryProfiles enriches authorized events only. Its bounded cache contains
// name/avatar, never retained email/username or authorization decisions.
type HistoryProfiles struct {
	reader   port.HistoryProfileReader
	mu       sync.Mutex
	cache    map[string]historyProfileCacheEntry
	sequence uint64
	now      func() time.Time
	slots    chan struct{}
}

// NewHistoryProfiles creates a process-local cache. A nil reader yields
// unavailable profiles, not failed history reads (for example in mock mode).
func NewHistoryProfiles(reader port.HistoryProfileReader) *HistoryProfiles {
	return &HistoryProfiles{reader: reader, cache: make(map[string]historyProfileCacheEntry), now: time.Now, slots: make(chan struct{}, constants.HistoryProfileConcurrency)}
}

// Enrich resolves each distinct user at most once per returned page, across
// both roles. Call only after history authorization. Directory failures affect
// display availability, never event visibility, retained identity or pagination.
func (h *HistoryProfiles) Enrich(ctx context.Context, events []model.HistoryEvent) {
	profiles := make(map[string]*model.HistoryDisplayProfile)
	for _, event := range events {
		for _, ref := range []string{event.Attribution.ActorRef, event.Attribution.EffectiveActorRef} {
			kind, _, valid := model.HistoryActorPrincipal(ref)
			if valid && kind == "user" {
				profiles[ref] = nil
			}
		}
	}
	misses := make([]string, 0, len(profiles))
	for ref := range profiles {
		if profile, hit := h.cached(ref); hit {
			profiles[ref] = &profile
		} else {
			misses = append(misses, ref)
		}
	}
	if h.reader != nil && len(misses) > 0 {
		lookupCtx, cancel := context.WithTimeout(ctx, constants.HistoryProfileTimeout)
		defer cancel()
		jobs := make(chan string, len(misses))
		for _, ref := range misses {
			jobs <- ref
		}
		close(jobs)
		var wg sync.WaitGroup
		var resultsMu sync.Mutex
		for range min(len(misses), constants.HistoryProfileConcurrency) {
			wg.Go(func() {
				for ref := range jobs {
					profile, ok := h.lookup(lookupCtx, ref)
					if ok {
						resultsMu.Lock()
						profiles[ref] = &profile
						resultsMu.Unlock()
					}
				}
			})
		}
		wg.Wait()
	}
	for i := range events {
		a := &events[i].Attribution
		a.ActorProfile = historyProfile(a.ActorRef, a.ActorIdentity, profiles[a.ActorRef])
		a.EffectiveActorProfile = historyProfile(a.EffectiveActorRef, a.EffectiveActorIdentity, profiles[a.EffectiveActorRef])
	}
}

func (h *HistoryProfiles) lookup(ctx context.Context, ref string) (model.HistoryDisplayProfile, bool) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return model.HistoryDisplayProfile{}, false
	}
	if ctx.Err() != nil {
		return model.HistoryDisplayProfile{}, false
	}
	_, principal, _ := model.HistoryActorPrincipal(ref)
	profile, err := h.reader.LookupHistoryProfile(ctx, principal)
	if err != nil {
		return model.HistoryDisplayProfile{}, false
	}
	if (profile.Name != nil && len(*profile.Name) > constants.HistoryProfileMaxNameBytes) ||
		(profile.AvatarURL != nil && len(*profile.AvatarURL) > constants.HistoryProfileMaxAvatarBytes) {
		return model.HistoryDisplayProfile{}, false
	}
	if !profile.Found {
		profile.Name, profile.AvatarURL = nil, nil
	}
	h.remember(ref, profile)
	return profile, true
}

func (h *HistoryProfiles) cached(ref string) (model.HistoryDisplayProfile, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.cache[ref]
	if !ok {
		return model.HistoryDisplayProfile{}, false
	}
	if !h.now().Before(entry.expires) {
		delete(h.cache, ref)
		return model.HistoryDisplayProfile{}, false
	}
	h.sequence++
	entry.used = h.sequence
	h.cache[ref] = entry
	return cloneHistoryDisplay(entry.profile), true
}

func (h *HistoryProfiles) remember(ref string, profile model.HistoryDisplayProfile) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for key, entry := range h.cache {
		if !now.Before(entry.expires) {
			delete(h.cache, key)
		}
	}
	if _, exists := h.cache[ref]; !exists && len(h.cache) >= constants.HistoryProfileCacheEntries {
		var oldest string
		var used uint64
		for key, entry := range h.cache {
			if oldest == "" || entry.used < used {
				oldest, used = key, entry.used
			}
		}
		delete(h.cache, oldest)
	}
	ttl := constants.HistoryProfileFoundTTL
	if !profile.Found {
		ttl = constants.HistoryProfileNotFoundTTL
	}
	h.sequence++
	h.cache[ref] = historyProfileCacheEntry{profile: cloneHistoryDisplay(profile), expires: now.Add(ttl), used: h.sequence}
}

func historyProfile(ref string, identity *model.HistoryIdentity, display *model.HistoryDisplayProfile) *model.HistoryProfile {
	kind, _, valid := model.HistoryActorPrincipal(ref)
	if valid && kind == "legacy" {
		// The reader has checked origin, false verification and null identity.
		// This label is presentation only, not a stored name or directory result.
		name := constants.HistoryUnknownLegacyUserName
		return &model.HistoryProfile{Availability: "unavailable", Name: &name}
	}
	if !valid || kind != "user" {
		return nil
	}
	profile := &model.HistoryProfile{Availability: "unavailable"}
	if identity != nil {
		profile.Email, profile.Username = copyHistoryString(identity.Email), copyHistoryString(identity.Username)
	}
	if display != nil {
		if display.Found {
			profile.Availability = "available"
			profile.Name, profile.AvatarURL = copyHistoryString(display.Name), copyHistoryString(display.AvatarURL)
		} else {
			profile.Availability = "deleted"
		}
	}
	return profile
}

func cloneHistoryDisplay(profile model.HistoryDisplayProfile) model.HistoryDisplayProfile {
	profile.Name, profile.AvatarURL = copyHistoryString(profile.Name), copyHistoryString(profile.AvatarURL)
	return profile
}

func copyHistoryString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// ResourceSearchOption configures optional resource-search dependencies.
type ResourceSearchOption func(*ResourceSearch)

// WithHistoryProfileReader enables optional live display enrichment without
// changing the history authorization or failure policy of current searches.
func WithHistoryProfileReader(reader port.HistoryProfileReader) ResourceSearchOption {
	return func(s *ResourceSearch) { s.historyProfiles = NewHistoryProfiles(reader) }
}
