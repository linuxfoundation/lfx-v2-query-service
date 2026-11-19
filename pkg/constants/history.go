// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

import "time"

const (
	// HistoryUserMetadataSubject is owned by auth-service's user_metadata contract.
	HistoryUserMetadataSubject = "lfx.auth-service.user_metadata.read"
	// HistoryProfileTimeout bounds the entire optional page-enrichment phase.
	HistoryProfileTimeout = 2 * time.Second
	// HistoryProfileFoundTTL bounds reuse of live display data.
	HistoryProfileFoundTTL = 10 * time.Minute
	// HistoryProfileNotFoundTTL bounds caching an explicit directory absence.
	HistoryProfileNotFoundTTL = time.Minute
	// HistoryProfileCacheEntries bounds the in-process personal-data cache.
	HistoryProfileCacheEntries = 4096
	// HistoryProfileConcurrency bounds directory I/O across all history requests.
	HistoryProfileConcurrency = 8
	// HistoryProfileMaxNameBytes bounds the directory display-name cache value.
	HistoryProfileMaxNameBytes = 1024
	// HistoryProfileMaxAvatarBytes bounds a live URL, never image bytes.
	HistoryProfileMaxAvatarBytes = 2048
)
