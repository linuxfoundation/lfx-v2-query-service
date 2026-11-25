// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// HistoryProfileReader resolves display fields by principal for already
// authorized history events. Implementations must honor context cancellation,
// omit identity/credentials from errors and never fetch remote avatar bytes.
type HistoryProfileReader interface {
	LookupHistoryProfile(ctx context.Context, principal string) (model.HistoryDisplayProfile, error)
}
