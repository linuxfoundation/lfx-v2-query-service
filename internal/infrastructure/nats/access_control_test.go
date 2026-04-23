// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
)

// MockNATSClient is a mock implementation of NATSClientInterface
type MockNATSClient struct {
	checkAccessResponse AccessCheckNATSResponse
	checkAccessError    error
	closeError          error
	isReadyError        error
	readTuplesResponse  *ReadTuplesNATSResponse
	readTuplesError     error
}

func NewMockNATSClient() *MockNATSClient {
	return &MockNATSClient{}
}

func (m *MockNATSClient) CheckAccess(ctx context.Context, request *AccessCheckNATSRequest) (AccessCheckNATSResponse, error) {
	if m.checkAccessError != nil {
		return nil, m.checkAccessError
	}
	return m.checkAccessResponse, nil
}

func (m *MockNATSClient) ReadTuples(ctx context.Context, request *ReadTuplesNATSRequest) (*ReadTuplesNATSResponse, error) {
	if m.readTuplesError != nil {
		return nil, m.readTuplesError
	}
	if m.readTuplesResponse != nil {
		return m.readTuplesResponse, nil
	}
	return &ReadTuplesNATSResponse{Results: []string{}}, nil
}

func (m *MockNATSClient) Close() error {
	return m.closeError
}

func (m *MockNATSClient) IsReady(ctx context.Context) error {
	return m.isReadyError
}

func (m *MockNATSClient) SetCheckAccessResponse(response AccessCheckNATSResponse) {
	m.checkAccessResponse = response
}

func (m *MockNATSClient) SetCheckAccessError(err error) {
	m.checkAccessError = err
}

func (m *MockNATSClient) SetCloseError(err error) {
	m.closeError = err
}

func (m *MockNATSClient) SetIsReadyError(err error) {
	m.isReadyError = err
}

func (m *MockNATSClient) SetReadTuplesResponse(response *ReadTuplesNATSResponse) {
	m.readTuplesResponse = response
}

func (m *MockNATSClient) SetReadTuplesError(err error) {
	m.readTuplesError = err
}

func TestNATSAccessControlChecker_CheckAccess(t *testing.T) {
	tests := []struct {
		name           string
		subject        string
		data           []byte
		timeout        time.Duration
		setupMock      func(*MockNATSClient)
		expectedError  bool
		expectedErrMsg string
		expectedResult model.AccessCheckResult
	}{
		{
			name:    "successful access check with allowed permissions",
			subject: "access.check.project",
			data:    []byte(`{"user_id": "user123", "resource": "project:abc"}`),
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessResponse(AccessCheckNATSResponse{
					"view":   "allowed",
					"edit":   "allowed",
					"delete": "denied",
				})
			},
			expectedError: false,
			expectedResult: model.AccessCheckResult{
				"view":   "allowed",
				"edit":   "allowed",
				"delete": "denied",
			},
		},
		{
			name:    "successful access check with denied permissions",
			subject: "access.check.project",
			data:    []byte(`{"user_id": "user456", "resource": "project:xyz"}`),
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessResponse(AccessCheckNATSResponse{
					"view":   "denied",
					"edit":   "denied",
					"delete": "denied",
				})
			},
			expectedError: false,
			expectedResult: model.AccessCheckResult{
				"view":   "denied",
				"edit":   "denied",
				"delete": "denied",
			},
		},
		{
			name:    "successful access check with empty response",
			subject: "access.check.project",
			data:    []byte(`{"user_id": "user789", "resource": "project:empty"}`),
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessResponse(AccessCheckNATSResponse{})
			},
			expectedError:  false,
			expectedResult: model.AccessCheckResult{},
		},
		{
			name:    "NATS client error",
			subject: "access.check.project",
			data:    []byte(`{"user_id": "user123", "resource": "project:abc"}`),
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessError(errors.New("NATS connection timeout"))
			},
			expectedError:  true,
			expectedErrMsg: "NATS access control check failed",
		},
		{
			name:    "empty subject",
			subject: "",
			data:    []byte(`{"user_id": "user123", "resource": "project:abc"}`),
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessError(errors.New("invalid NATS access check request: subject and message must be set"))
			},
			expectedError:  true,
			expectedErrMsg: "NATS access control check failed",
		},
		{
			name:    "nil data",
			subject: "access.check.project",
			data:    nil,
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessError(errors.New("invalid NATS access check request: subject and message must be set"))
			},
			expectedError:  true,
			expectedErrMsg: "NATS access control check failed",
		},
		{
			name:    "empty data",
			subject: "access.check.project",
			data:    []byte{},
			timeout: 5 * time.Second,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessError(errors.New("invalid NATS access check request: subject and message must be set"))
			},
			expectedError:  true,
			expectedErrMsg: "NATS access control check failed",
		},
		{
			name:    "zero timeout",
			subject: "access.check.project",
			data:    []byte(`{"user_id": "user123", "resource": "project:abc"}`),
			timeout: 0,
			setupMock: func(mock *MockNATSClient) {
				mock.SetCheckAccessResponse(AccessCheckNATSResponse{
					"view": "allowed",
				})
			},
			expectedError: false,
			expectedResult: model.AccessCheckResult{
				"view": "allowed",
			},
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockClient := NewMockNATSClient()
			tc.setupMock(mockClient)

			// Create access control checker
			checker := &NATSAccessControlChecker{
				client: mockClient,
			}

			// Execute
			ctx := context.Background()
			result, err := checker.CheckAccess(ctx, tc.subject, tc.data, tc.timeout)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				return
			}

			assertion.NoError(err)
			assertion.Equal(tc.expectedResult, result)
		})
	}
}

func TestNATSAccessControlChecker_Close(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*MockNATSClient)
		expectedError  bool
		expectedErrMsg string
	}{
		{
			name: "successful close",
			setupMock: func(mock *MockNATSClient) {
				// No error on close
			},
			expectedError: false,
		},
		{
			name: "close with error",
			setupMock: func(mock *MockNATSClient) {
				mock.SetCloseError(errors.New("failed to close connection"))
			},
			expectedError:  true,
			expectedErrMsg: "failed to close connection",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockClient := NewMockNATSClient()
			tc.setupMock(mockClient)

			// Create access control checker
			checker := &NATSAccessControlChecker{
				client: mockClient,
			}

			// Execute
			err := checker.Close()

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				return
			}

			assertion.NoError(err)
		})
	}
}

func TestNATSAccessControlChecker_convertFromNATSResponse(t *testing.T) {
	tests := []struct {
		name           string
		natsResponse   AccessCheckNATSResponse
		expectedResult model.AccessCheckResult
	}{
		{
			name: "convert response with multiple permissions",
			natsResponse: AccessCheckNATSResponse{
				"view":   "allowed",
				"edit":   "allowed",
				"delete": "denied",
				"admin":  "denied",
			},
			expectedResult: model.AccessCheckResult{
				"view":   "allowed",
				"edit":   "allowed",
				"delete": "denied",
				"admin":  "denied",
			},
		},
		{
			name: "convert response with single permission",
			natsResponse: AccessCheckNATSResponse{
				"view": "allowed",
			},
			expectedResult: model.AccessCheckResult{
				"view": "allowed",
			},
		},
		{
			name:           "convert empty response",
			natsResponse:   AccessCheckNATSResponse{},
			expectedResult: model.AccessCheckResult{},
		},
		{
			name:           "convert nil response",
			natsResponse:   nil,
			expectedResult: model.AccessCheckResult(nil),
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create access control checker
			checker := &NATSAccessControlChecker{
				client: NewMockNATSClient(),
			}

			// Execute
			result := checker.convertFromNATSResponse(tc.natsResponse)

			// Verify
			assertion.Equal(tc.expectedResult, result)
		})
	}
}

func TestNATSAccessControlChecker_IsReady(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*MockNATSClient)
		expectedError  bool
		expectedErrMsg string
	}{
		{
			name: "successful ready check",
			setupMock: func(mock *MockNATSClient) {
				// No error on IsReady
			},
			expectedError: false,
		},
		{
			name: "ready check with error",
			setupMock: func(mock *MockNATSClient) {
				mock.SetIsReadyError(errors.New("NATS server not available"))
			},
			expectedError:  true,
			expectedErrMsg: "NATS server not available",
		},
		{
			name: "ready check with connection timeout",
			setupMock: func(mock *MockNATSClient) {
				mock.SetIsReadyError(errors.New("connection timeout"))
			},
			expectedError:  true,
			expectedErrMsg: "connection timeout",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock
			mockClient := NewMockNATSClient()
			tc.setupMock(mockClient)

			// Create access control checker
			checker := &NATSAccessControlChecker{
				client: mockClient,
			}

			// Execute
			ctx := context.Background()
			err := checker.IsReady(ctx)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				return
			}

			assertion.NoError(err)
		})
	}
}

func TestNewAccessControlChecker(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedError  bool
		expectedErrMsg string
	}{
		{
			name: "create access control checker with valid config",
			config: Config{
				URL:           "nats://localhost:4222",
				Timeout:       5 * time.Second,
				MaxReconnect:  10,
				ReconnectWait: 2 * time.Second,
			},
			expectedError: false,
		},
		{
			name: "create access control checker with empty URL",
			config: Config{
				URL:           "",
				Timeout:       5 * time.Second,
				MaxReconnect:  10,
				ReconnectWait: 2 * time.Second,
			},
			expectedError:  true,
			expectedErrMsg: "failed to create NATS client",
		},
		{
			name: "create access control checker with invalid URL",
			config: Config{
				URL:           "invalid-url",
				Timeout:       5 * time.Second,
				MaxReconnect:  10,
				ReconnectWait: 2 * time.Second,
			},
			expectedError:  true,
			expectedErrMsg: "failed to create NATS client",
		},
		{
			name: "create access control checker with zero timeout",
			config: Config{
				URL:           "nats://localhost:4222",
				Timeout:       0,
				MaxReconnect:  10,
				ReconnectWait: 2 * time.Second,
			},
			expectedError: false,
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			ctx := context.Background()
			checker, err := NewAccessControlChecker(ctx, tc.config)

			// Verify
			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				assertion.Nil(checker)
				return
			}

			// Note: For successful cases, we can't easily test without a real NATS server
			// In a real scenario, this would fail because there's no NATS server running
			// But we can check the basic structure
			if err == nil {
				assertion.NotNil(checker)
				assertion.IsType(&NATSAccessControlChecker{}, checker)
				// Clean up
				errClose := checker.Close()
				assertion.NoError(errClose, "failed to close NATS client")
			}
		})
	}
}

func TestNATSAccessControlChecker_ReadTuples(t *testing.T) {
	tests := []struct {
		name            string
		setupMock       func(*MockNATSClient)
		user            string
		objectType      string
		expectedRefs    []string
		expectedError   bool
		expectedErrMsg  string
	}{
		{
			name: "happy path - returns deduplicated object refs",
			setupMock: func(m *MockNATSClient) {
				m.SetReadTuplesResponse(&ReadTuplesNATSResponse{
					Results: []string{
						"v1_past_meeting:meeting-1#invitee@user:jme",
						"v1_past_meeting:meeting-2#attendee@user:jme",
						"v1_past_meeting:meeting-1#attendee@user:jme", // duplicate object
					},
				})
			},
			user:          "user:jme",
			objectType:    "v1_past_meeting",
			expectedRefs:  []string{"v1_past_meeting:meeting-1", "v1_past_meeting:meeting-2"},
			expectedError: false,
		},
		{
			name: "empty results returns empty slice",
			setupMock: func(m *MockNATSClient) {
				m.SetReadTuplesResponse(&ReadTuplesNATSResponse{
					Results: []string{},
				})
			},
			user:          "user:jme",
			objectType:    "v1_past_meeting",
			expectedRefs:  []string{},
			expectedError: false,
		},
		{
			name: "malformed tuple strings without # are skipped",
			setupMock: func(m *MockNATSClient) {
				m.SetReadTuplesResponse(&ReadTuplesNATSResponse{
					Results: []string{
						"v1_past_meeting:meeting-1#invitee@user:jme",
						"malformed-no-hash",
						"v1_past_meeting:meeting-2#attendee@user:jme",
					},
				})
			},
			user:          "user:jme",
			objectType:    "v1_past_meeting",
			expectedRefs:  []string{"v1_past_meeting:meeting-1", "v1_past_meeting:meeting-2"},
			expectedError: false,
		},
		{
			name: "client error is propagated",
			setupMock: func(m *MockNATSClient) {
				m.SetReadTuplesError(errors.New("nats timeout"))
			},
			user:           "user:jme",
			objectType:     "v1_past_meeting",
			expectedError:  true,
			expectedErrMsg: "NATS read_tuples failed",
		},
		{
			name: "fga-sync error in response is propagated",
			setupMock: func(m *MockNATSClient) {
				m.SetReadTuplesError(errors.New("read_tuples error from fga-sync: internal error"))
			},
			user:           "user:jme",
			objectType:     "v1_past_meeting",
			expectedError:  true,
			expectedErrMsg: "NATS read_tuples failed",
		},
	}

	assertion := assert.New(t)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := NewMockNATSClient()
			tc.setupMock(mockClient)

			checker := &NATSAccessControlChecker{client: mockClient}
			ctx := context.Background()

			refs, err := checker.ReadTuples(ctx, tc.user, tc.objectType, 5*time.Second)

			if tc.expectedError {
				assertion.Error(err)
				assertion.Contains(err.Error(), tc.expectedErrMsg)
				assertion.Nil(refs)
				return
			}
			assertion.NoError(err)
			assertion.ElementsMatch(tc.expectedRefs, refs)
		})
	}
}

func TestNATSAccessControlChecker_Integration(t *testing.T) {
	assertion := assert.New(t)

	t.Run("end-to-end access control flow", func(t *testing.T) {
		// Setup mock with realistic data
		mockClient := NewMockNATSClient()
		mockClient.SetCheckAccessResponse(AccessCheckNATSResponse{
			"view":   "allowed",
			"edit":   "allowed",
			"delete": "denied",
			"admin":  "denied",
		})

		// Create access control checker
		checker := &NATSAccessControlChecker{
			client: mockClient,
		}

		// Execute access check
		ctx := context.Background()
		subject := "access.check.project"
		data := []byte(`{
			"user_id": "user123",
			"resource": "project:integration-test",
			"relations": ["view", "edit", "delete", "admin"]
		}`)
		timeout := 5 * time.Second

		result, err := checker.CheckAccess(ctx, subject, data, timeout)

		// Verify
		assertion.NoError(err)
		assertion.NotNil(result)
		assertion.Equal("allowed", result["view"])
		assertion.Equal("allowed", result["edit"])
		assertion.Equal("denied", result["delete"])
		assertion.Equal("denied", result["admin"])

		// Test close
		err = checker.Close()
		assertion.NoError(err)
	})
}
