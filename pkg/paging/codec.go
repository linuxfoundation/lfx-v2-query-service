// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package paging

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"golang.org/x/crypto/nacl/secretbox"
)

// DecodePageToken takes a base64-encoded, secretbox-encrypted token and returns the searchAfter string.
// Returns an error if decoding, decryption, or unmarshaling fails.
func DecodePageToken(ctx context.Context, encoded string, secretKey *[32]byte) (string, error) {

	slog.DebugContext(ctx, "decoding page token")

	encrypted, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.NewValidation("invalid encoded page token", err)
	}

	if len(encrypted) < constants.NonceSize+secretbox.Overhead {
		return "", errors.NewValidation(
			"invalid page token length",
			fmt.Errorf("expected at least %d bytes, got %d", constants.NonceSize+secretbox.Overhead, len(encrypted)),
		)
	}

	var decryptNonce [constants.NonceSize]byte
	copy(decryptNonce[:], encrypted[:constants.NonceSize])
	decrypted, ok := secretbox.Open(nil, encrypted[constants.NonceSize:], &decryptNonce, secretKey)
	if !ok {
		return "", errors.NewValidation("failed to decrypt page token")
	}

	// JSON re-marshal to normalize structure.
	searchAfterMsg := json.RawMessage(string(decrypted))
	searchAfterData, err := json.Marshal(searchAfterMsg)
	if err != nil {
		return "", errors.NewValidation("failed to marshal search_after data", err)
	}

	slog.DebugContext(ctx, "decoded page token successfully")

	return string(searchAfterData), nil
}

// EncodePageToken takes a JSON-serializable value (e.g., []interface{}, map[string]interface{}, etc),
// encrypts with secretbox, and returns a secure base64 token.
func EncodePageToken(searchAfter any, secretKey *[32]byte) (string, error) {
	encodedSearchAfter, err := json.Marshal(searchAfter)
	if err != nil {
		return "", errors.NewUnexpected("failed to marshal search_after data", err)

	}

	var nonce [constants.NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", errors.NewUnexpected("failed to generate nonce for page token", err)
	}

	encrypted := secretbox.Seal(nonce[:], encodedSearchAfter, &nonce, secretKey)

	token := base64.RawURLEncoding.EncodeToString(encrypted)
	return token, nil
}
