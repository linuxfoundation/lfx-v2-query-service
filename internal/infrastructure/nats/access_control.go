// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
)

// NATSAccessControlChecker implements the AccessControlChecker interface for NATS
type NATSAccessControlChecker struct {
	client NATSClientInterface
}

// CheckAccess implements the AccessControlChecker interface
func (n *NATSAccessControlChecker) CheckAccess(ctx context.Context, subj string, data []byte, timeout time.Duration) (model.AccessCheckResult, error) {
	slog.DebugContext(ctx, "executing NATS access control check",
		"subject", subj,
		"timeout", timeout,
		"message", string(data),
	)

	// Send request via NATS
	response, err := n.client.CheckAccess(ctx, &AccessCheckNATSRequest{
		Subject: subj,
		Message: data,
		Timeout: timeout,
	})
	if err != nil {
		slog.ErrorContext(ctx, "NATS access control check failed",
			"error", err,
			"subject", subj,
		)
		return nil, fmt.Errorf("NATS access control check failed: %w", err)
	}

	// Convert NATS response to domain response
	result := n.convertFromNATSResponse(response)

	slog.DebugContext(ctx, "NATS access control check completed",
		"subject", subj,
		"result", result,
	)

	return result, nil
}

// ReadTuples retrieves the object refs that a user has direct FGA relationships to
func (n *NATSAccessControlChecker) ReadTuples(ctx context.Context, user string, objectType string, timeout time.Duration) ([]string, error) {
	slog.DebugContext(ctx, "reading FGA tuples",
		"user", user,
		"object_type", objectType,
	)

	response, err := n.client.ReadTuples(ctx, &ReadTuplesNATSRequest{
		User:       user,
		ObjectType: objectType,
		Timeout:    timeout,
	})
	if err != nil {
		slog.ErrorContext(ctx, "NATS read_tuples failed",
			"error", err,
			"user", user,
			"object_type", objectType,
		)
		return nil, fmt.Errorf("NATS read_tuples failed: %w", err)
	}
	if response == nil {
		return []string{}, nil
	}

	// Parse results from "object#relation@user" format, extracting the object ref
	seen := make(map[string]struct{}, len(response.Results))
	objectRefs := make([]string, 0, len(response.Results))
	for _, result := range response.Results {
		// Extract the object part (everything before the first '#')
		objectRef, _, found := strings.Cut(result, "#")
		if !found || objectRef == "" {
			continue
		}
		if _, ok := seen[objectRef]; ok {
			continue
		}
		seen[objectRef] = struct{}{}
		objectRefs = append(objectRefs, objectRef)
	}

	slog.DebugContext(ctx, "FGA tuples read",
		"user", user,
		"object_type", objectType,
		"count", len(objectRefs),
	)

	return objectRefs, nil
}

// Close gracefully closes the NATS connection
func (n *NATSAccessControlChecker) Close() error {
	return n.client.Close()
}

func (n *NATSAccessControlChecker) IsReady(ctx context.Context) error {
	if err := n.client.IsReady(ctx); err != nil {
		return err
	}
	return nil
}

// convertFromNATSResponse converts NATS response to domain response
func (n *NATSAccessControlChecker) convertFromNATSResponse(response AccessCheckNATSResponse) model.AccessCheckResult {
	return model.AccessCheckResult(response)
}

// NewAccessControlChecker creates a new NATS access control checker
func NewAccessControlChecker(ctx context.Context, config Config) (port.AccessControlChecker, error) {
	slog.InfoContext(ctx, "creating NATS access control checker",
		"url", config.URL,
	)

	client, err := NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS client: %w", err)
	}

	return &NATSAccessControlChecker{
		client: client,
	}, nil
}
