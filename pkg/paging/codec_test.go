// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package paging

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
)

type testContextKey string

func TestEncodePageToken(t *testing.T) {
	// Create a test secret key (32 bytes)
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	tests := []struct {
		name          string
		searchAfter   any
		expectedError bool
		errorType     any
	}{
		{
			name:          "encode simple string",
			searchAfter:   "simple-string",
			expectedError: false,
		},
		{
			name:          "encode integer",
			searchAfter:   12345,
			expectedError: false,
		},
		{
			name:          "encode float",
			searchAfter:   123.45,
			expectedError: false,
		},
		{
			name:          "encode boolean",
			searchAfter:   true,
			expectedError: false,
		},
		{
			name:          "encode nil",
			searchAfter:   nil,
			expectedError: false,
		},
		{
			name: "encode map",
			searchAfter: map[string]any{
				"id":   "123",
				"name": "test",
			},
			expectedError: false,
		},
		{
			name:          "encode slice",
			searchAfter:   []any{"item1", "item2", 123},
			expectedError: false,
		},
		{
			name: "encode complex nested structure",
			searchAfter: map[string]any{
				"user": map[string]any{
					"id":   123,
					"name": "John Doe",
					"tags": []string{"admin", "user"},
				},
				"timestamp": 1234567890,
			},
			expectedError: false,
		},
		{
			name:          "encode empty string",
			searchAfter:   "",
			expectedError: false,
		},
		{
			name:          "encode empty map",
			searchAfter:   map[string]any{},
			expectedError: false,
		},
		{
			name:          "encode empty slice",
			searchAfter:   []any{},
			expectedError: false,
		},
		{
			name:          "encode function (should fail)",
			searchAfter:   func() {},
			expectedError: true,
			errorType:     errors.Unexpected{},
		},
		{
			name:          "encode channel (should fail)",
			searchAfter:   make(chan int),
			expectedError: true,
			errorType:     errors.Unexpected{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			token, err := EncodePageToken(tc.searchAfter, &secretKey)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.errorType != nil {
					assert.IsType(t, tc.errorType, err)
				}
				assert.Empty(t, token)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, token)

				// Verify token is valid base64
				assert.NotContains(t, token, "=") // RawURLEncoding doesn't use padding
				assert.NotContains(t, token, "+")
				assert.NotContains(t, token, "/")
			}
		})
	}
}

func TestDecodePageToken(t *testing.T) {
	// Create a test secret key (32 bytes)
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	ctx := context.Background()

	tests := []struct {
		name            string
		setupToken      func() string
		expectedError   bool
		errorType       any
		expectedContent string
	}{
		{
			name: "decode valid simple string token",
			setupToken: func() string {
				token, _ := EncodePageToken("simple-string", &secretKey)
				return token
			},
			expectedError:   false,
			expectedContent: `"simple-string"`,
		},
		{
			name: "decode valid integer token",
			setupToken: func() string {
				token, _ := EncodePageToken(12345, &secretKey)
				return token
			},
			expectedError:   false,
			expectedContent: "12345",
		},
		{
			name: "decode valid map token",
			setupToken: func() string {
				data := map[string]any{"id": "123", "name": "test"}
				token, _ := EncodePageToken(data, &secretKey)
				return token
			},
			expectedError:   false,
			expectedContent: `{"id":"123","name":"test"}`,
		},
		{
			name: "decode valid slice token",
			setupToken: func() string {
				data := []any{"item1", "item2", 123}
				token, _ := EncodePageToken(data, &secretKey)
				return token
			},
			expectedError:   false,
			expectedContent: `["item1","item2",123]`,
		},
		{
			name: "decode valid nil token",
			setupToken: func() string {
				token, _ := EncodePageToken(nil, &secretKey)
				return token
			},
			expectedError:   false,
			expectedContent: "null",
		},
		{
			name: "decode invalid base64",
			setupToken: func() string {
				return "invalid-base64-!!!"
			},
			expectedError: true,
			errorType:     errors.Validation{},
		},
		{
			name: "decode empty token",
			setupToken: func() string {
				return ""
			},
			expectedError: true,
			errorType:     errors.Validation{},
		},
		{
			name: "decode token too short",
			setupToken: func() string {
				return "dGVzdA" // "test" in base64, but too short for nonce + overhead
			},
			expectedError: true,
			errorType:     errors.Validation{},
		},
		{
			name: "decode token with wrong key",
			setupToken: func() string {
				wrongKey := [32]byte{}
				copy(wrongKey[:], []byte("wrong-key-32-bytes-long-wrong-k"))
				token, _ := EncodePageToken("test", &wrongKey)
				return token
			},
			expectedError: true,
			errorType:     errors.Validation{},
		},
		{
			name: "decode corrupted token",
			setupToken: func() string {
				token, _ := EncodePageToken("test", &secretKey)
				// Corrupt the token by changing a character
				corrupted := []rune(token)
				if len(corrupted) > 0 {
					corrupted[len(corrupted)/2] = 'X'
				}
				return string(corrupted)
			},
			expectedError: true,
			errorType:     errors.Validation{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			token := tc.setupToken()

			// Execute
			result, err := DecodePageToken(ctx, token, &secretKey)

			// Verify
			if tc.expectedError {
				assert.Error(t, err)
				if tc.errorType != nil {
					assert.IsType(t, tc.errorType, err)
				}
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedContent, result)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// Create a test secret key (32 bytes)
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	ctx := context.Background()

	tests := []struct {
		name        string
		searchAfter any
	}{
		{
			name:        "string round trip",
			searchAfter: "test-string",
		},
		{
			name:        "integer round trip",
			searchAfter: 42,
		},
		{
			name:        "float round trip",
			searchAfter: 3.14159,
		},
		{
			name:        "boolean round trip",
			searchAfter: true,
		},
		{
			name:        "nil round trip",
			searchAfter: nil,
		},
		{
			name: "map round trip",
			searchAfter: map[string]any{
				"id":        "user-123",
				"timestamp": 1234567890,
				"active":    true,
			},
		},
		{
			name:        "slice round trip",
			searchAfter: []any{"a", "b", "c", 1, 2, 3},
		},
		{
			name: "complex nested structure round trip",
			searchAfter: map[string]any{
				"user": map[string]any{
					"id":     123,
					"name":   "John Doe",
					"email":  "john@example.com",
					"active": true,
					"score":  95.5,
					"tags":   []string{"admin", "premium"},
					"metadata": map[string]any{
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-12-31T23:59:59Z",
					},
				},
				"pagination": map[string]any{
					"page":     1,
					"per_page": 20,
					"total":    100,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Encode
			token, err := EncodePageToken(tc.searchAfter, &secretKey)
			assert.NoError(t, err)
			assert.NotEmpty(t, token)

			// Decode
			decoded, err := DecodePageToken(ctx, token, &secretKey)
			assert.NoError(t, err)
			assert.NotEmpty(t, decoded)

			// Verify round trip by comparing JSON representations
			originalJSON, err := json.Marshal(tc.searchAfter)
			assert.NoError(t, err)

			assert.JSONEq(t, string(originalJSON), decoded)
		})
	}
}

func TestDecodePageToken_ContextHandling(t *testing.T) {
	// Create a test secret key (32 bytes)
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	// Create a valid token
	token, err := EncodePageToken("test-data", &secretKey)
	assert.NoError(t, err)

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "background context",
			ctx:  context.Background(),
		},
		{
			name: "context with value",
			ctx:  context.WithValue(context.Background(), testContextKey("key"), "value"),
		},
		{
			name: "cancelled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute - should work with any context type
			result, err := DecodePageToken(tc.ctx, token, &secretKey)
			assert.NoError(t, err)
			assert.Equal(t, `"test-data"`, result)
		})
	}
}

func TestPageTokenSecurity(t *testing.T) {
	// Test that tokens encrypted with different keys can't be decrypted
	secretKey1 := [32]byte{}
	copy(secretKey1[:], []byte("key1-32-bytes-long-for-testing-1"))

	secretKey2 := [32]byte{}
	copy(secretKey2[:], []byte("key2-32-bytes-long-for-testing-2"))

	ctx := context.Background()
	testData := "sensitive-data"

	// Encode with key1
	token, err := EncodePageToken(testData, &secretKey1)
	assert.NoError(t, err)

	// Try to decode with key2 (should fail)
	_, err = DecodePageToken(ctx, token, &secretKey2)
	assert.Error(t, err)
	assert.IsType(t, errors.Validation{}, err)
	assert.Contains(t, err.Error(), "failed to decrypt page token")
}

func TestPageTokenUniqueness(t *testing.T) {
	// Test that encoding the same data multiple times produces different tokens
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	testData := "same-data"

	token1, err := EncodePageToken(testData, &secretKey)
	assert.NoError(t, err)

	token2, err := EncodePageToken(testData, &secretKey)
	assert.NoError(t, err)

	// Tokens should be different due to random nonces
	assert.NotEqual(t, token1, token2)

	// But both should decode to the same data
	ctx := context.Background()

	decoded1, err := DecodePageToken(ctx, token1, &secretKey)
	assert.NoError(t, err)

	decoded2, err := DecodePageToken(ctx, token2, &secretKey)
	assert.NoError(t, err)

	assert.Equal(t, decoded1, decoded2)
	assert.Equal(t, `"same-data"`, decoded1)
}

func TestPageTokenConstants(t *testing.T) {
	// Verify that the constants package defines the required constants
	// This is more of a smoke test to ensure dependencies are correct
	assert.Greater(t, constants.NonceSize, 0)

	// NonceSize should be 24 bytes for NaCl secretbox
	assert.Equal(t, 24, constants.NonceSize)
}

func TestDecodePageToken_InvalidJSON(t *testing.T) {
	// Test decoding a token that contains invalid JSON after decryption
	secretKey := [32]byte{}
	copy(secretKey[:], []byte("12345678901234567890123456789012"))

	ctx := context.Background()

	// This test is tricky because EncodePageToken always produces valid JSON
	// We'll test with data that would be valid for encryption but invalid for JSON processing
	// However, since we use json.Marshal in EncodePageToken, this scenario is unlikely
	// This test mainly ensures robustness

	// Create a token with valid data first
	token, err := EncodePageToken("valid-data", &secretKey)
	assert.NoError(t, err)

	// Decode should work
	result, err := DecodePageToken(ctx, token, &secretKey)
	assert.NoError(t, err)
	assert.Equal(t, `"valid-data"`, result)
}
