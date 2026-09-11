// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import (
	"context"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// AccessControlChecker defines the interface for access control operations
// This abstraction allows different access control implementations (NATS, etc.)
// without the domain layer knowing about specific implementations
type AccessControlChecker interface {
	// CheckAccess verifies if a user has permission to access specific resources
	CheckAccess(ctx context.Context, subj string, data []byte, timeout time.Duration) (model.AccessCheckResult, error)

	// ReadTuples returns the object refs (e.g. "v1_past_meeting:123") that a user
	// has direct FGA relationships to, filtered by objectType.
	ReadTuples(ctx context.Context, user string, objectType string, timeout time.Duration) ([]string, error)

	// Close gracefully closes the access control checker connection
	Close() error

	// IsReady checks if the access control service is ready to process requests
	IsReady(ctx context.Context) error
}
