// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import (
	"context"
	"errors"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// ErrAccessCheckPayloadTooLarge indicates that an access-check chunk
// exceeded the transport's maximum message size. This is a deterministic,
// non-retryable failure: the chunk will be rejected identically on every
// attempt, so callers should not retry it. Adapters wrap their
// transport-specific "too large" error with this sentinel so the service
// layer can recognize it without depending on a specific transport.
var ErrAccessCheckPayloadTooLarge = errors.New("access check payload exceeds the transport's maximum message size")

// AccessControlChecker defines the interface for access control operations
// This abstraction allows different access control implementations (NATS, etc.)
// without the domain layer knowing about specific implementations
type AccessControlChecker interface {
	// CheckAccess verifies if a user has permission to access specific resources
	CheckAccess(ctx context.Context, subj string, data []byte, timeout time.Duration) (model.AccessCheckResult, error)

	// ReadTuples returns the object refs (e.g. "v1_past_meeting:123") that a user
	// has direct FGA relationships to, filtered by objectType.
	ReadTuples(ctx context.Context, user string, objectType string, timeout time.Duration) ([]string, error)

	// MaxPayload returns the transport's actual negotiated maximum message
	// size, or 0 if unknown. Callers sizing an outbound batch must not assume
	// a fixed constant: the real ceiling depends on how the connected server
	// is configured and can be smaller than any client-side default.
	MaxPayload() int64

	// Close gracefully closes the access control checker connection
	Close() error

	// IsReady checks if the access control service is ready to process requests
	IsReady(ctx context.Context) error
}
