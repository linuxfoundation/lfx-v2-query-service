// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package global

import (
	"context"
	"log"
	"log/slog"
	"os"
	"sync"
)

var (
	pageTokenSecret       [32]byte
	doOncePageTokenSecret sync.Once
)

// PageTokenSecret retrieves the secret used for encoding and decoding page tokens.
func PageTokenSecret(ctx context.Context) *[32]byte {

	doOncePageTokenSecret.Do(func() {

		const pageTokenSecretName = "PAGE_TOKEN_SECRET"

		pageTokenSecretValue := os.Getenv(pageTokenSecretName)
		if pageTokenSecretValue == "" {
			slog.ErrorContext(ctx, "missing environment variable")
			log.Fatalf("environment variable %s must be set with 32 characters", pageTokenSecretName)
		}
		copy(pageTokenSecret[:], []byte(pageTokenSecretValue))
	})

	return &pageTokenSecret
}
