// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package global

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPageTokenSecret(t *testing.T) {
	tests := []struct {
		name           string
		envVarValue    string
		setupEnv       func(*testing.T, string)
		expectedLength int
		expectedError  bool
		validateSecret func(*testing.T, *[32]byte, string)
	}{
		{
			name:        "successful retrieval with exactly 32 bytes",
			envVarValue: "this-is-a-test-secret-32-bytes!!",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			expectedLength: 32,
			expectedError:  false,
			validateSecret: func(t *testing.T, secret *[32]byte, expectedValue string) {
				assert.Equal(t, []byte(expectedValue), secret[:])
			},
		},
		{
			name:        "successful retrieval with short secret (zero padded)",
			envVarValue: "short",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			expectedLength: 32,
			expectedError:  false,
			validateSecret: func(t *testing.T, secret *[32]byte, expectedValue string) {
				expectedBytes := []byte(expectedValue)
				secretBytes := secret[:]

				// Check the first part matches
				assert.Equal(t, expectedBytes, secretBytes[:len(expectedBytes)])

				// Check the rest is zero-padded
				for i := len(expectedBytes); i < 32; i++ {
					assert.Equal(t, byte(0), secretBytes[i])
				}
			},
		},
		{
			name:        "successful retrieval with long secret (truncated)",
			envVarValue: "this-is-a-very-long-secret-that-exceeds-32-bytes-and-should-be-truncated-properly",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			expectedLength: 32,
			expectedError:  false,
			validateSecret: func(t *testing.T, secret *[32]byte, expectedValue string) {
				expectedBytes := []byte(expectedValue)[:32]
				assert.Equal(t, expectedBytes, secret[:])
			},
		},
		{
			name:        "successful retrieval with special characters",
			envVarValue: "!@#$%^&*()_+-=[]{}|;':\",./<>?",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			expectedLength: 32,
			expectedError:  false,
			validateSecret: func(t *testing.T, secret *[32]byte, expectedValue string) {
				expectedBytes := []byte(expectedValue)
				secretBytes := secret[:]

				// Check the first part matches
				assert.Equal(t, expectedBytes, secretBytes[:len(expectedBytes)])

				// Check the rest is zero-padded
				for i := len(expectedBytes); i < 32; i++ {
					assert.Equal(t, byte(0), secretBytes[i])
				}
			},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset global state for each test
			resetGlobalState()

			// Setup environment
			tc.setupEnv(t, tc.envVarValue)

			// Execute
			ctx := context.Background()
			secret := PageTokenSecret(ctx)

			// Verify
			if tc.expectedError {
				assertion.Nil(secret)
				return
			}

			assertion.NotNil(secret)
			assertion.Equal(tc.expectedLength, len(secret))

			if tc.validateSecret != nil {
				tc.validateSecret(t, secret, tc.envVarValue)
			}
		})
	}
}

func TestPageTokenSecretIdempotency(t *testing.T) {
	tests := []struct {
		name        string
		envVarValue string
		callCount   int
		setupEnv    func(*testing.T, string)
	}{
		{
			name:        "multiple calls return same instance",
			envVarValue: "idempotency-test-secret-value!!",
			callCount:   3,
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
		},
		{
			name:        "many calls return same instance",
			envVarValue: "many-calls-test-secret-value!!!",
			callCount:   10,
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset global state for each test
			resetGlobalState()

			// Setup environment
			tc.setupEnv(t, tc.envVarValue)

			ctx := context.Background()
			secrets := make([]*[32]byte, tc.callCount)

			// Execute multiple calls
			for i := 0; i < tc.callCount; i++ {
				secrets[i] = PageTokenSecret(ctx)
			}

			// Verify all calls return the same pointer
			for i := 1; i < tc.callCount; i++ {
				assertion.Equal(secrets[0], secrets[i], "Call %d should return the same pointer as call 0", i)
			}

			// Verify all calls return the same content
			for i := 1; i < tc.callCount; i++ {
				assertion.Equal(*secrets[0], *secrets[i], "Call %d should return the same content as call 0", i)
			}
		})
	}
}

func TestPageTokenSecretConcurrency(t *testing.T) {
	tests := []struct {
		name         string
		envVarValue  string
		goroutines   int
		setupEnv     func(*testing.T, string)
		validateSync func(*testing.T, []*[32]byte, string)
	}{
		{
			name:        "concurrent access with 10 goroutines",
			envVarValue: "concurrent-test-secret-value!!!",
			goroutines:  10,
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			validateSync: func(t *testing.T, results []*[32]byte, expectedValue string) {
				// All results should be the same pointer (sync.Once ensures this)
				for i := 1; i < len(results); i++ {
					assert.Equal(t, results[0], results[i], "Goroutine %d should return the same pointer", i)
				}

				// Verify content is correct
				expectedBytes := []byte(expectedValue)
				secretBytes := results[0][:]
				assert.Equal(t, expectedBytes, secretBytes[:len(expectedBytes)])
			},
		},
		{
			name:        "concurrent access with 50 goroutines",
			envVarValue: "high-concurrency-test-secret!!!!",
			goroutines:  50,
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			validateSync: func(t *testing.T, results []*[32]byte, expectedValue string) {
				// All results should be the same pointer
				for i := 1; i < len(results); i++ {
					assert.Equal(t, results[0], results[i], "Goroutine %d should return the same pointer", i)
				}
			},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset global state for each test
			resetGlobalState()

			// Setup environment
			tc.setupEnv(t, tc.envVarValue)

			ctx := context.Background()
			results := make([]*[32]byte, tc.goroutines)
			var wg sync.WaitGroup

			// Execute concurrent calls
			for i := 0; i < tc.goroutines; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					// Add small delay to increase chance of concurrent execution
					time.Sleep(time.Millisecond * time.Duration(index%10))
					results[index] = PageTokenSecret(ctx)
				}(i)
			}

			// Wait for all goroutines to complete
			wg.Wait()

			// Verify results
			assertion.NotNil(results[0])
			if tc.validateSync != nil {
				tc.validateSync(t, results, tc.envVarValue)
			}
		})
	}
}

func TestPageTokenSecretContextVariations(t *testing.T) {

	type key string
	var (
		testKey1 = key("testKey1")
		testKey2 = key("testKey2")
	)

	tests := []struct {
		name         string
		envVarValue  string
		setupEnv     func(*testing.T, string)
		createCtx    func() context.Context
		expectResult bool
	}{
		{
			name:        "context.Background()",
			envVarValue: "context-background-test-secret!",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			createCtx:    context.Background,
			expectResult: true,
		},
		{
			name:        "context.TODO()",
			envVarValue: "context-todo-test-secret-value!",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			createCtx:    context.TODO,
			expectResult: true,
		},
		{
			name:        "context with values",
			envVarValue: "context-with-values-test-secret",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			createCtx: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, testKey1, "value1")
				ctx = context.WithValue(ctx, testKey2, "value2")
				return ctx
			},
			expectResult: true,
		},
		{
			name:        "cancelled context",
			envVarValue: "cancelled-context-test-secret!!",
			setupEnv: func(t *testing.T, value string) {
				t.Setenv("PAGE_TOKEN_SECRET", value)
			},
			createCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return ctx
			},
			expectResult: true, // Function should still work with cancelled context
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset global state for each test
			resetGlobalState()

			// Setup environment
			tc.setupEnv(t, tc.envVarValue)

			// Execute
			ctx := tc.createCtx()
			secret := PageTokenSecret(ctx)

			// Verify
			if tc.expectResult {
				assertion.NotNil(secret)
				assertion.Equal(32, len(secret))
			} else {
				assertion.Nil(secret)
			}
		})
	}
}

// BenchmarkPageTokenSecret benchmarks the performance of PageTokenSecret
func BenchmarkPageTokenSecret(b *testing.B) {
	benchmarks := []struct {
		name        string
		envVarValue string
		setupEnv    func(*testing.B, string)
	}{
		{
			name:        "single call performance",
			envVarValue: "benchmark-test-secret-value!!!",
			setupEnv: func(b *testing.B, value string) {
				b.Setenv("PAGE_TOKEN_SECRET", value)
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Reset global state
			resetGlobalState()

			// Setup environment
			bm.setupEnv(b, bm.envVarValue)

			ctx := context.Background()

			// Run benchmark
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				PageTokenSecret(ctx)
			}
		})
	}
}

// BenchmarkPageTokenSecretConcurrent benchmarks concurrent access performance
func BenchmarkPageTokenSecretConcurrent(b *testing.B) {
	benchmarks := []struct {
		name        string
		envVarValue string
		setupEnv    func(*testing.B, string)
	}{
		{
			name:        "concurrent access performance",
			envVarValue: "concurrent-benchmark-secret!!!!!",
			setupEnv: func(b *testing.B, value string) {
				b.Setenv("PAGE_TOKEN_SECRET", value)
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Reset global state
			resetGlobalState()

			// Setup environment
			bm.setupEnv(b, bm.envVarValue)

			ctx := context.Background()

			// Run concurrent benchmark
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					PageTokenSecret(ctx)
				}
			})
		})
	}
}

// resetGlobalState resets the global state for testing
// This is necessary because the sync.Once ensures the function only runs once
func resetGlobalState() {
	pageTokenSecret = [32]byte{}
	doOncePageTokenSecret = sync.Once{}
}
