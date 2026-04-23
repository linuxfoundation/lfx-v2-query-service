// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mock

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
)

// MockAccessControlChecker provides a mock implementation of AccessControlChecker for testing
type MockAccessControlChecker struct {
	// PublicResourcesOnly determines if only public resources should be allowed
	PublicResourcesOnly bool
	// AllowedUserIDs contains user IDs that should be granted access to all resources
	AllowedUserIDs []string
	// DeniedResourceIDs contains resource IDs that should always be denied
	DeniedResourceIDs []string
	// SimulateErrors determines if errors should be simulated
	SimulateErrors bool
	// DefaultResult is the default access result ("allowed" or "denied")
	DefaultResult string
	// Test helper fields
	checkAccessResponse map[string]string
	checkAccessError    error
	isReadyError        error
	// MockTupleRefs is the list of object refs returned by ReadTuples
	MockTupleRefs []string
	// SimulateTuplesError determines if ReadTuples should return an error
	SimulateTuplesError bool
}

// CheckAccess implements the AccessControlChecker interface with mock behavior
func (m *MockAccessControlChecker) CheckAccess(ctx context.Context, subj string, data []byte, timeout time.Duration) (model.AccessCheckResult, error) {
	slog.DebugContext(ctx, "executing mock access control check",
		"subject", subj,
		"timeout", timeout,
		"message", string(data),
		"public_only", m.PublicResourcesOnly,
	)

	// If test has set a mock error, return it
	if m.checkAccessError != nil {
		return nil, m.checkAccessError
	}

	// If test has set a mock response, return it
	if m.checkAccessResponse != nil {
		return m.checkAccessResponse, nil
	}

	result := make(model.AccessCheckResult)

	// Parse the input data - expecting line-separated permission requests
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Convert to string for processing
		request := string(line)

		// Simulate error for specific requests if configured
		if m.SimulateErrors && strings.Contains(request, "error") {
			result[request] = "error"
			continue
		}

		// Check if request is explicitly denied
		if m.isRequestDenied(request) {
			result[request] = "denied"
			continue
		}

		// Grant access based on mock rules
		if m.shouldGrantAccess(request) {
			result[request] = "true"
		} else {
			result[request] = "false"
		}
	}

	slog.DebugContext(ctx, "mock access control check completed",
		"subject", subj,
		"result_count", len(result),
		"result", result,
	)

	return result, nil
}

// ReadTuples implements the AccessControlChecker interface with mock behavior
func (m *MockAccessControlChecker) ReadTuples(_ context.Context, _ string, _ string, _ time.Duration) ([]string, error) {
	if m.SimulateTuplesError {
		return nil, fmt.Errorf("mock read_tuples error")
	}
	if m.MockTupleRefs != nil {
		return m.MockTupleRefs, nil
	}
	return []string{}, nil
}

// Close implements the AccessControlChecker interface (no-op for mock)
func (m *MockAccessControlChecker) Close() error {
	return nil
}

// IsReady implements the AccessControlChecker interface (always ready for mock)
func (m *MockAccessControlChecker) IsReady(ctx context.Context) error {
	if m.isReadyError != nil {
		return m.isReadyError
	}
	return nil
}

// isRequestDenied checks if the request is explicitly denied
func (m *MockAccessControlChecker) isRequestDenied(request string) bool {
	for _, deniedID := range m.DeniedResourceIDs {
		if strings.Contains(request, deniedID) {
			return true
		}
	}
	return false
}

// shouldGrantAccess determines if access should be granted based on mock rules
func (m *MockAccessControlChecker) shouldGrantAccess(request string) bool {
	// If we have a default result set, use it
	if m.DefaultResult != "" {
		return m.DefaultResult == "allowed"
	}

	// Check for public resources
	if strings.Contains(request, "public:true") {
		return true
	}

	// If we're in public-only mode and request is not public, deny access
	if m.PublicResourcesOnly && !strings.Contains(request, "public:true") {
		return false
	}

	// Check for allowed user IDs in the request
	for _, allowedID := range m.AllowedUserIDs {
		if strings.Contains(request, allowedID) {
			return true
		}
	}

	// Default behavior: grant access to admin/maintainer roles, deny others
	return strings.Contains(request, "admin") ||
		strings.Contains(request, "maintainer") ||
		strings.Contains(request, "allowed")
}

// NewMockAccessControlChecker creates a new mock access control checker
func NewMockAccessControlChecker() *MockAccessControlChecker {
	return &MockAccessControlChecker{
		PublicResourcesOnly: false,
		AllowedUserIDs:      []string{"admin", "test-user"},
		DeniedResourceIDs:   []string{},
		SimulateErrors:      false,
		DefaultResult:       "allowed", // Default to allowing access in mock
	}
}

// NewMockAccessControlCheckerPublicOnly creates a mock that only allows public resources
func NewMockAccessControlCheckerPublicOnly() *MockAccessControlChecker {
	return &MockAccessControlChecker{
		PublicResourcesOnly: true,
		AllowedUserIDs:      []string{},
		DeniedResourceIDs:   []string{},
		SimulateErrors:      false,
		DefaultResult:       "",
	}
}

// NewMockAccessControlCheckerWithErrors creates a mock that simulates errors
func NewMockAccessControlCheckerWithErrors() *MockAccessControlChecker {
	return &MockAccessControlChecker{
		PublicResourcesOnly: false,
		AllowedUserIDs:      []string{"admin"},
		DeniedResourceIDs:   []string{},
		SimulateErrors:      true,
		DefaultResult:       "",
	}
}

// NewMockAccessControlCheckerDenyAll creates a mock that denies all access
func NewMockAccessControlCheckerDenyAll() *MockAccessControlChecker {
	return &MockAccessControlChecker{
		PublicResourcesOnly: false,
		AllowedUserIDs:      []string{},
		DeniedResourceIDs:   []string{},
		SimulateErrors:      false,
		DefaultResult:       "denied",
	}
}

// Test helper methods for setting up mock responses

// SetCheckAccessResponse sets the mock response for CheckAccess calls
func (m *MockAccessControlChecker) SetCheckAccessResponse(response map[string]string) {
	m.checkAccessResponse = response
}

// SetCheckAccessError sets the mock error for CheckAccess calls
func (m *MockAccessControlChecker) SetCheckAccessError(err error) {
	m.checkAccessError = err
}

// SetIsReadyError sets the mock error for IsReady calls
func (m *MockAccessControlChecker) SetIsReadyError(err error) {
	m.isReadyError = err
}
