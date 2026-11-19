// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	infraMock "github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
)

type historyCapableNATSClient struct {
	NATSClientInterface
	*infraMock.HistoryProfileReader
}

func TestNATSAccessControlChecker_LookupHistoryProfile(t *testing.T) {
	reader := &infraMock.HistoryProfileReader{}
	reader.On("LookupHistoryProfile", mock.Anything, "testuser01").Return(model.HistoryDisplayProfile{Found: true}, nil).Once()
	checker := &NATSAccessControlChecker{client: &historyCapableNATSClient{HistoryProfileReader: reader}}
	profile, err := checker.LookupHistoryProfile(context.Background(), "testuser01")
	require.NoError(t, err)
	require.True(t, profile.Found)
	reader.AssertExpectations(t)
}

func TestLookupHistoryProfile(t *testing.T) {
	for _, tt := range []struct {
		name, reply          string
		err                  error
		found                bool
		wantName, wantAvatar string
		wantErr              bool
	}{
		{name: "available", reply: `{"success":true,"created_at":"2026-01-01T00:00:00Z","data":{"name":"Test User 1","picture":"https://example.com/avatar.png","email":"not-retained@example.com","username":"not-retained"}}`, found: true, wantName: "Test User 1", wantAvatar: "https://example.com/avatar.png"},
		{name: "partial", reply: `{"success":true,"data":{"name":null}}`, found: true},
		{name: "deleted", reply: `{"success":false,"error":"user not found"}`},
		{name: "unavailable", reply: `{"success":false,"error":"private-response"}`, wantErr: true},
		{name: "no responders", err: natsgo.ErrNoResponders, wantErr: true},
		{name: "timeout", err: context.DeadlineExceeded, wantErr: true},
		{name: "driver details", err: fmt.Errorf("private-response"), wantErr: true},
		{name: "malformed", reply: `{`, wantErr: true},
		{name: "missing success", reply: `{"data":{}}`, wantErr: true},
		{name: "null success", reply: `{"success":null}`, wantErr: true},
		{name: "missing data", reply: `{"success":true}`, wantErr: true},
		{name: "null data", reply: `{"success":true,"data":null}`, wantErr: true},
		{name: "wrong display type", reply: `{"success":true,"data":{"name":3}}`, wantErr: true},
		{name: "contradictory success", reply: `{"success":true,"error":"private-response","data":{}}`, wantErr: true},
		{name: "contradictory absence", reply: `{"success":false,"error":"user not found","data":{}}`, wantErr: true},
		{name: "duplicate success", reply: `{"success":true,"success":false,"error":"user not found"}`, wantErr: true},
		{name: "noncanonical success", reply: `{"Success":false,"error":"user not found"}`, wantErr: true},
		{name: "trailing JSON", reply: `{"success":false,"error":"user not found"}{}`, wantErr: true},
		{name: "invalid utf8", reply: "{\"success\":true,\"data\":{\"name\":\"\xff\"}}", wantErr: true},
		{name: "oversized", reply: strings.Repeat("x", (1<<20)+1), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport := &infraMock.HistoryProfileRequester{}
			transport.On("RequestWithContext", mock.Anything, constants.HistoryUserMetadataSubject, []byte("testuser01")).Run(func(args mock.Arguments) {
				deadline, ok := args.Get(0).(context.Context).Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), constants.HistoryProfileTimeout)
			}).Return(&natsgo.Msg{Data: []byte(tt.reply)}, tt.err).Once()
			profile, err := lookupHistoryProfile(context.Background(), transport, "testuser01")
			if tt.wantErr {
				require.EqualError(t, err, "history profile lookup unavailable")
				require.Equal(t, model.HistoryDisplayProfile{}, profile)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.found, profile.Found)
				if tt.wantName != "" {
					require.Equal(t, tt.wantName, *profile.Name)
				} else {
					require.Nil(t, profile.Name)
				}
				if tt.wantAvatar != "" {
					require.Equal(t, tt.wantAvatar, *profile.AvatarURL)
				} else {
					require.Nil(t, profile.AvatarURL)
				}
			}
			transport.AssertExpectations(t)
		})
	}
	for _, principal := range []string{"", "user:testuser01", "test user", "testuser\n", strings.Repeat("x", 257)} {
		transport := &infraMock.HistoryProfileRequester{}
		_, err := lookupHistoryProfile(context.Background(), transport, principal)
		require.Error(t, err)
		transport.AssertNotCalled(t, "RequestWithContext", mock.Anything, mock.Anything, mock.Anything)
	}
	_, err := (&NATSClient{}).LookupHistoryProfile(context.Background(), "testuser01")
	require.Error(t, err)
	_, err = (&NATSAccessControlChecker{}).LookupHistoryProfile(context.Background(), "testuser01")
	require.Error(t, err)
}
