// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	infraMock "github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
)

func historyString(value string) *string { return &value }

func identityHistoryEvent(actor, effective string) model.HistoryEvent {
	return model.HistoryEvent{Attribution: model.HistoryAttribution{
		ActorRef: "user:" + actor, EffectiveActorRef: "user:" + effective,
		ActorVerified: true, EffectiveActorVerified: false,
		ActorIdentity:          &model.HistoryIdentity{Username: historyString(actor), Email: historyString(actor + "@example.com")},
		EffectiveActorIdentity: &model.HistoryIdentity{Username: historyString(effective), Email: historyString(effective + "@example.com")},
	}}
}

func TestHistoryProfiles_Enrich(t *testing.T) {
	for _, tt := range []struct {
		name         string
		profile      model.HistoryDisplayProfile
		err          error
		availability string
	}{
		{"available", model.HistoryDisplayProfile{Found: true, Name: historyString("Test User 1"), AvatarURL: historyString("https://example.com/avatar.png")}, nil, "available"},
		{"partial", model.HistoryDisplayProfile{Found: true}, nil, "available"},
		{"deleted", model.HistoryDisplayProfile{}, nil, "deleted"},
		{"unavailable", model.HistoryDisplayProfile{}, fmt.Errorf("private-response"), "unavailable"},
		{"oversized name unavailable", model.HistoryDisplayProfile{Found: true, Name: historyString(strings.Repeat("x", constants.HistoryProfileMaxNameBytes+1))}, nil, "unavailable"},
		{"oversized avatar unavailable", model.HistoryDisplayProfile{Found: true, AvatarURL: historyString(strings.Repeat("x", constants.HistoryProfileMaxAvatarBytes+1))}, nil, "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &infraMock.HistoryProfileReader{}
			reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(tt.profile, tt.err).Once()
			enricher := NewHistoryProfiles(reader)
			events := []model.HistoryEvent{identityHistoryEvent("testuser01", "testuser01")}
			enricher.Enrich(context.Background(), events)
			a := events[0].Attribution
			for _, p := range []*model.HistoryProfile{a.ActorProfile, a.EffectiveActorProfile} {
				require.Equal(t, tt.availability, p.Availability)
				require.Equal(t, "testuser01@example.com", *p.Email)
				require.Equal(t, "testuser01", *p.Username)
				if tt.availability == "available" {
					require.Equal(t, tt.profile.Name, p.Name)
					require.Equal(t, tt.profile.AvatarURL, p.AvatarURL)
				} else {
					require.Nil(t, p.Name)
					require.Nil(t, p.AvatarURL)
				}
			}
			require.True(t, a.ActorVerified)
			require.False(t, a.EffectiveActorVerified, "read-time lookup must not change verification")
			*a.ActorProfile.Email = "changed-response@example.com"
			require.Equal(t, "testuser01@example.com", *a.ActorIdentity.Email, "response mutation cannot change retained identity")
			require.Equal(t, "testuser01@example.com", *a.EffectiveActorProfile.Email)
			reader.AssertExpectations(t)
		})
	}
	t.Run("nil reader is unavailable", func(t *testing.T) {
		events := []model.HistoryEvent{identityHistoryEvent("testuser01", "testuser01")}
		NewHistoryProfiles(nil).Enrich(context.Background(), events)
		require.Equal(t, "unavailable", events[0].Attribution.ActorProfile.Availability)
	})
	t.Run("service actors have no profiles", func(t *testing.T) {
		reader := &infraMock.HistoryProfileReader{}
		events := []model.HistoryEvent{{Attribution: model.HistoryAttribution{ActorRef: "service:fixture", EffectiveActorRef: "service:fixture", ActorVerified: true, EffectiveActorVerified: true}}}
		NewHistoryProfiles(reader).Enrich(context.Background(), events)
		require.Nil(t, events[0].Attribution.ActorProfile)
		require.Nil(t, events[0].Attribution.EffectiveActorProfile)
		reader.AssertNotCalled(t, "LookupHistoryProfile", mock.Anything, mock.Anything)
	})
}

func TestHistoryProfiles_EnrichDeduplicatesAndCaches(t *testing.T) {
	reader := &infraMock.HistoryProfileReader{}
	for i := range 3 {
		reader.On("LookupHistoryProfile", mock.Anything, fmt.Sprintf("testuser%02d", i)).Return(model.HistoryDisplayProfile{Found: true, Name: historyString(fmt.Sprintf("Test User %d", i))}, nil).Once()
	}
	enricher := NewHistoryProfiles(reader)
	for range 2 {
		events := make([]model.HistoryEvent, 100)
		for i := range events {
			events[i] = identityHistoryEvent(fmt.Sprintf("testuser%02d", i%3), fmt.Sprintf("testuser%02d", (i+1)%3))
		}
		enricher.Enrich(context.Background(), events)
		for i, event := range events {
			require.Equal(t, fmt.Sprintf("Test User %d", i%3), *event.Attribution.ActorProfile.Name)
			require.Equal(t, fmt.Sprintf("Test User %d", (i+1)%3), *event.Attribution.EffectiveActorProfile.Name)
		}
	}
	reader.AssertNumberOfCalls(t, "LookupHistoryProfile", 3)
	reader.AssertExpectations(t)
}

func TestHistoryProfiles_CacheExpiry(t *testing.T) {
	for _, tt := range []struct {
		name  string
		found bool
		ttl   time.Duration
	}{
		{"found", true, constants.HistoryProfileFoundTTL}, {"not found", false, constants.HistoryProfileNotFoundTTL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &infraMock.HistoryProfileReader{}
			reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(model.HistoryDisplayProfile{Found: tt.found}, nil).Once()
			reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(model.HistoryDisplayProfile{Found: true, Name: historyString("Updated Test User")}, nil).Once()
			enricher := NewHistoryProfiles(reader)
			now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
			enricher.now = func() time.Time { return now }
			events := []model.HistoryEvent{identityHistoryEvent("testuser01", "testuser01")}
			enricher.Enrich(context.Background(), events)
			now = now.Add(tt.ttl - time.Nanosecond)
			enricher.Enrich(context.Background(), events)
			reader.AssertNumberOfCalls(t, "LookupHistoryProfile", 1)
			now = now.Add(time.Nanosecond)
			enricher.Enrich(context.Background(), events)
			require.Equal(t, "Updated Test User", *events[0].Attribution.ActorProfile.Name)
			reader.AssertExpectations(t)
		})
	}
}

func TestHistoryProfiles_DoesNotCacheFailures(t *testing.T) {
	reader := &infraMock.HistoryProfileReader{}
	reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(model.HistoryDisplayProfile{}, fmt.Errorf("unavailable")).Once()
	reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(model.HistoryDisplayProfile{Found: true}, nil).Once()
	enricher := NewHistoryProfiles(reader)
	events := []model.HistoryEvent{identityHistoryEvent("testuser01", "testuser01"), identityHistoryEvent("testuser01", "testuser01")}
	enricher.Enrich(context.Background(), events)
	require.Equal(t, "unavailable", events[0].Attribution.ActorProfile.Availability)
	enricher.Enrich(context.Background(), events)
	require.Equal(t, "available", events[0].Attribution.ActorProfile.Availability)
	reader.AssertNumberOfCalls(t, "LookupHistoryProfile", 2)
}

func TestHistoryProfiles_CacheBoundAndCopies(t *testing.T) {
	enricher := NewHistoryProfiles(nil)
	now := time.Now()
	enricher.now = func() time.Time { return now }
	for i := range constants.HistoryProfileCacheEntries {
		enricher.cache[fmt.Sprintf("user:testuser%04d", i)] = historyProfileCacheEntry{profile: model.HistoryDisplayProfile{Found: true}, expires: now.Add(time.Minute), used: uint64(i + 1)}
	}
	enricher.sequence = constants.HistoryProfileCacheEntries
	_, hit := enricher.cached("user:testuser0000")
	require.True(t, hit)
	name := "Test User"
	enricher.remember("user:newfixture", model.HistoryDisplayProfile{Found: true, Name: &name})
	require.Len(t, enricher.cache, constants.HistoryProfileCacheEntries)
	_, hit = enricher.cache["user:testuser0000"]
	require.True(t, hit)
	_, hit = enricher.cache["user:testuser0001"]
	require.False(t, hit, "evict least recently used")
	name = "Changed Caller"
	profile, hit := enricher.cached("user:newfixture")
	require.True(t, hit)
	require.Equal(t, "Test User", *profile.Name)
	*profile.Name = "Changed Result"
	profile, _ = enricher.cached("user:newfixture")
	require.Equal(t, "Test User", *profile.Name)
	now = now.Add(11 * time.Minute)
	enricher.remember("user:otherfixture", model.HistoryDisplayProfile{Found: false})
	require.Len(t, enricher.cache, 1, "prune expired entries before retaining new personal data")
}

func TestHistoryProfiles_EnrichmentDeadlineAndConcurrency(t *testing.T) {
	reader := &infraMock.HistoryProfileReader{}
	var active, peak, calls atomic.Int32
	started := make(chan struct{})
	var once sync.Once
	reader.On("LookupHistoryProfile", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		calls.Add(1)
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		once.Do(func() { close(started) })
		<-args.Get(0).(context.Context).Done()
		active.Add(-1)
	}).Return(model.HistoryDisplayProfile{}, context.DeadlineExceeded)
	events := make([]model.HistoryEvent, 100)
	for i := range events {
		principal := fmt.Sprintf("testuser%03d", i)
		events[i] = identityHistoryEvent(principal, principal)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { NewHistoryProfiles(reader).Enrich(ctx, events); close(done) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("enrichment did not start a directory request")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("page failures must not serialize hundreds of lookup timeouts")
	}
	require.Positive(t, peak.Load())
	require.LessOrEqual(t, peak.Load(), int32(constants.HistoryProfileConcurrency))
	require.LessOrEqual(t, calls.Load(), int32(constants.HistoryProfileConcurrency), "queued actors become unavailable without new I/O after deadline")
	for _, event := range events {
		require.Equal(t, "unavailable", event.Attribution.ActorProfile.Availability)
		require.NotNil(t, event.Attribution.ActorProfile.Email)
	}
}
