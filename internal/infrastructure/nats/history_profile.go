// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"unicode/utf8"

	natsgo "github.com/nats-io/nats.go"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

type historyProfileRequester interface {
	RequestWithContext(context.Context, string, []byte) (*natsgo.Msg, error)
}

var (
	_ port.HistoryProfileReader = (*NATSClient)(nil)
	_ port.HistoryProfileReader = (*NATSAccessControlChecker)(nil)
)

// LookupHistoryProfile reuses the access checker's NATS connection, including
// its normal shutdown path. Profile availability never affects readiness.
func (n *NATSAccessControlChecker) LookupHistoryProfile(ctx context.Context, principal string) (model.HistoryDisplayProfile, error) {
	reader, ok := n.client.(port.HistoryProfileReader)
	if !ok {
		return model.HistoryDisplayProfile{}, errors.NewServiceUnavailable("history profile lookup unavailable")
	}
	return reader.LookupHistoryProfile(ctx, principal)
}

// LookupHistoryProfile reads only name/picture from auth-service metadata.
// Raw principal requests deliberately avoid requestWithSpan: personal directory
// data, responses and upstream error strings must not reach logs or tracing.
func (c *NATSClient) LookupHistoryProfile(ctx context.Context, principal string) (model.HistoryDisplayProfile, error) {
	if c.conn == nil {
		return model.HistoryDisplayProfile{}, errors.NewServiceUnavailable("history profile lookup unavailable")
	}
	return lookupHistoryProfile(ctx, c.conn, principal)
}

func lookupHistoryProfile(ctx context.Context, requester historyProfileRequester, principal string) (model.HistoryDisplayProfile, error) {
	unavailable := func() (model.HistoryDisplayProfile, error) {
		return model.HistoryDisplayProfile{}, errors.NewServiceUnavailable("history profile lookup unavailable")
	}
	if _, _, ok := model.HistoryActorPrincipal("user:" + principal); !ok {
		return unavailable()
	}
	ctx, cancel := context.WithTimeout(ctx, constants.HistoryProfileTimeout)
	defer cancel()
	message, err := requester.RequestWithContext(ctx, constants.HistoryUserMetadataSubject, []byte(principal))
	if err != nil || message == nil || len(message.Data) > 1<<20 || !utf8.Valid(message.Data) {
		return unavailable()
	}
	// Close the reply envelope against duplicate keys and trailing JSON while
	// allowing additive metadata fields owned by auth-service. Retain neither
	// the arbitrary directory object nor any of its identity/contact fields.
	decoder := json.NewDecoder(bytes.NewReader(message.Data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return unavailable()
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return unavailable()
		}
		if _, exists := fields[key]; exists {
			return unavailable()
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return unavailable()
		}
		fields[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return unavailable()
	}
	var success *bool
	var failure string
	if json.Unmarshal(fields["success"], &success) != nil || success == nil {
		return unavailable()
	}
	if raw, present := fields["error"]; present && json.Unmarshal(raw, &failure) != nil {
		return unavailable()
	}
	var data *struct {
		Name    *string `json:"name"`
		Picture *string `json:"picture"`
	}
	if raw, present := fields["data"]; present && json.Unmarshal(raw, &data) != nil {
		return unavailable()
	}
	if !*success {
		if failure == "user not found" && data == nil {
			return model.HistoryDisplayProfile{Found: false}, nil
		}
		return unavailable()
	}
	if failure != "" || data == nil {
		return unavailable()
	}
	return model.HistoryDisplayProfile{Found: true, Name: data.Name, AvatarURL: data.Picture}, nil
}
