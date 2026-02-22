// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"errors"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	pkgerrors "github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
	"github.com/stretchr/testify/assert"
)

type testContextKey string

func TestWrapError(t *testing.T) {
	tests := []struct {
		name                 string
		inputError           error
		expectedErrorType    interface{}
		expectedErrorMessage string
	}{
		{
			name:                 "validation error",
			inputError:           pkgerrors.NewValidation("invalid input", nil),
			expectedErrorType:    &querysvc.BadRequestError{},
			expectedErrorMessage: "invalid input",
		},
		{
			name:                 "validation error with wrapped error",
			inputError:           pkgerrors.NewValidation("validation failed", errors.New("underlying error")),
			expectedErrorType:    &querysvc.BadRequestError{},
			expectedErrorMessage: "validation failed: underlying error",
		},
		{
			name:                 "not found error",
			inputError:           pkgerrors.NewNotFound("resource not found", nil),
			expectedErrorType:    &querysvc.NotFoundError{},
			expectedErrorMessage: "resource not found",
		},
		{
			name:                 "not found error with wrapped error",
			inputError:           pkgerrors.NewNotFound("resource not found", errors.New("db error")),
			expectedErrorType:    &querysvc.NotFoundError{},
			expectedErrorMessage: "resource not found: db error",
		},
		{
			name:                 "service unavailable error",
			inputError:           pkgerrors.NewServiceUnavailable("service down", nil),
			expectedErrorType:    &querysvc.ServiceUnavailableError{},
			expectedErrorMessage: "service down",
		},
		{
			name:                 "service unavailable error with wrapped error",
			inputError:           pkgerrors.NewServiceUnavailable("service unavailable", errors.New("connection refused")),
			expectedErrorType:    &querysvc.ServiceUnavailableError{},
			expectedErrorMessage: "service unavailable: connection refused",
		},
		{
			name:                 "generic error becomes internal server error",
			inputError:           errors.New("generic error"),
			expectedErrorType:    &querysvc.InternalServerError{},
			expectedErrorMessage: "generic error",
		},
		{
			name:                 "custom error becomes internal server error",
			inputError:           &customError{message: "custom error occurred"},
			expectedErrorType:    &querysvc.InternalServerError{},
			expectedErrorMessage: "custom error occurred",
		},
		{
			name:                 "nil error becomes internal server error",
			inputError:           nil,
			expectedErrorType:    &querysvc.InternalServerError{},
			expectedErrorMessage: "unknown error", // This is how nil errors are handled
		},
		{
			name:                 "unexpected error becomes internal server error",
			inputError:           pkgerrors.NewUnexpected("unexpected error", nil),
			expectedErrorType:    &querysvc.InternalServerError{},
			expectedErrorMessage: "unexpected error",
		},
		{
			name:                 "unexpected error becomes internal server error",
			inputError:           pkgerrors.NewUnexpected("unexpected client error", nil),
			expectedErrorType:    &querysvc.InternalServerError{},
			expectedErrorMessage: "unexpected client error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Execute
			result := wrapError(ctx, tc.inputError)

			// Verify error type
			assert.IsType(t, tc.expectedErrorType, result)

			// Verify error message - check the Message field since Error() returns empty string
			switch typedErr := result.(type) {
			case *querysvc.BadRequestError:
				assert.Contains(t, typedErr.Message, tc.expectedErrorMessage)
			case *querysvc.NotFoundError:
				assert.Contains(t, typedErr.Message, tc.expectedErrorMessage)
			case *querysvc.ServiceUnavailableError:
				assert.Contains(t, typedErr.Message, tc.expectedErrorMessage)
			case *querysvc.InternalServerError:
				assert.Contains(t, typedErr.Message, tc.expectedErrorMessage)
			default:
				t.Errorf("Unexpected error type: %T", result)
			}

			// Verify specific error types have correct structure
			switch expectedErr := tc.expectedErrorType.(type) {
			case *querysvc.BadRequestError:
				if badReqErr, ok := result.(*querysvc.BadRequestError); ok {
					assert.Equal(t, tc.expectedErrorMessage, badReqErr.Message)
				}
			case *querysvc.NotFoundError:
				if notFoundErr, ok := result.(*querysvc.NotFoundError); ok {
					assert.Equal(t, tc.expectedErrorMessage, notFoundErr.Message)
				}
			case *querysvc.ServiceUnavailableError:
				if svcUnavailErr, ok := result.(*querysvc.ServiceUnavailableError); ok {
					assert.Equal(t, tc.expectedErrorMessage, svcUnavailErr.Message)
				}
			case *querysvc.InternalServerError:
				if internalErr, ok := result.(*querysvc.InternalServerError); ok {
					assert.Equal(t, tc.expectedErrorMessage, internalErr.Message)
				}
			default:
				t.Errorf("Unexpected error type: %T", expectedErr)
			}
		})
	}
}

func TestWrapError_ErrorMapping(t *testing.T) {
	// Test specific error type mappings
	ctx := context.Background()

	// Test Validation -> BadRequestError
	validationErr := pkgerrors.NewValidation("test validation", nil)
	wrappedErr := wrapError(ctx, validationErr)
	_, ok := wrappedErr.(*querysvc.BadRequestError)
	assert.True(t, ok, "Validation error should map to BadRequestError")

	// Test NotFound -> NotFoundError
	notFoundErr := pkgerrors.NewNotFound("test not found", nil)
	wrappedErr = wrapError(ctx, notFoundErr)
	_, ok = wrappedErr.(*querysvc.NotFoundError)
	assert.True(t, ok, "NotFound error should map to NotFoundError")

	// Test ServiceUnavailable -> ServiceUnavailableError
	serviceUnavailableErr := pkgerrors.NewServiceUnavailable("test service unavailable", nil)
	wrappedErr = wrapError(ctx, serviceUnavailableErr)
	_, ok = wrappedErr.(*querysvc.ServiceUnavailableError)
	assert.True(t, ok, "ServiceUnavailable error should map to ServiceUnavailableError")

	// Test any other error -> InternalServerError
	genericErr := errors.New("generic error")
	wrappedErr = wrapError(ctx, genericErr)
	_, ok = wrappedErr.(*querysvc.InternalServerError)
	assert.True(t, ok, "Generic error should map to InternalServerError")
}

func TestWrapError_PreservesOriginalMessage(t *testing.T) {
	tests := []struct {
		name              string
		originalError     error
		expectedInMessage string
	}{
		{
			name:              "simple validation error",
			originalError:     pkgerrors.NewValidation("field is required", nil),
			expectedInMessage: "field is required",
		},
		{
			name:              "validation error with cause",
			originalError:     pkgerrors.NewValidation("validation failed", errors.New("field cannot be empty")),
			expectedInMessage: "validation failed: field cannot be empty",
		},
		{
			name:              "not found with details",
			originalError:     pkgerrors.NewNotFound("user with id 123 not found", nil),
			expectedInMessage: "user with id 123 not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			wrappedErr := wrapError(ctx, tc.originalError)
			// Check the Message field since Error() returns empty string
			switch typedErr := wrappedErr.(type) {
			case *querysvc.BadRequestError:
				assert.Contains(t, typedErr.Message, tc.expectedInMessage)
			case *querysvc.NotFoundError:
				assert.Contains(t, typedErr.Message, tc.expectedInMessage)
			case *querysvc.ServiceUnavailableError:
				assert.Contains(t, typedErr.Message, tc.expectedInMessage)
			case *querysvc.InternalServerError:
				assert.Contains(t, typedErr.Message, tc.expectedInMessage)
			default:
				t.Errorf("Unexpected error type: %T", wrappedErr)
			}
		})
	}
}

func TestWrapError_ContextHandling(t *testing.T) {
	// Test that the function works with different context types
	tests := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{
			name: "background context",
			ctx:  context.Background(),
			err:  pkgerrors.NewValidation("test", nil),
		},
		{
			name: "context with value",
			ctx:  context.WithValue(context.Background(), testContextKey("key"), "value"),
			err:  pkgerrors.NewNotFound("test", nil),
		},
		{
			name: "cancelled context",
			ctx:  func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(),
			err:  pkgerrors.NewServiceUnavailable("test", nil),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic or fail with different context types
			result := wrapError(tc.ctx, tc.err)
			assert.NotNil(t, result)
		})
	}
}

// Custom error type for testing
type customError struct {
	message string
}

func (e *customError) Error() string {
	return e.message
}

func TestWrapError_NilHandling(t *testing.T) {
	ctx := context.Background()

	// Test with nil error (should not panic)
	result := wrapError(ctx, nil)
	assert.NotNil(t, result)
	assert.IsType(t, &querysvc.InternalServerError{}, result)
}

func TestWrapError_ComplexErrorChain(t *testing.T) {
	// Test with complex error chains
	ctx := context.Background()

	// Create a chain of errors
	rootErr := errors.New("root cause")
	middleErr := pkgerrors.NewValidation("middle error", rootErr)

	result := wrapError(ctx, middleErr)

	assert.IsType(t, &querysvc.BadRequestError{}, result)
	if badReqErr, ok := result.(*querysvc.BadRequestError); ok {
		assert.Contains(t, badReqErr.Message, "middle error")
		assert.Contains(t, badReqErr.Message, "root cause")
	}
}

func TestWrapError_ErrorTypeAssertions(t *testing.T) {
	ctx := context.Background()

	// Test that we can properly assert the wrapped error types
	validationErr := pkgerrors.NewValidation("validation error", nil)
	wrapped := wrapError(ctx, validationErr)

	if badReqErr, ok := wrapped.(*querysvc.BadRequestError); ok {
		assert.Equal(t, "validation error", badReqErr.Message)
	} else {
		t.Error("Expected BadRequestError type assertion to succeed")
	}

	// Test that wrong type assertions fail
	_, ok := wrapped.(*querysvc.NotFoundError)
	assert.False(t, ok, "Wrong type assertion should fail")
}
