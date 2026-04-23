// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"log/slog"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

func wrapError(ctx context.Context, err error) error {

	f := func(err error) error {
		if err == nil {
			return &querysvc.InternalServerError{
				Message: "unknown error",
			}
		}

		var validation errors.Validation
		var notFound errors.NotFound
		var serviceUnavailable errors.ServiceUnavailable

		switch {
		case stderrors.As(err, &validation):
			return &querysvc.BadRequestError{
				Message: validation.Error(),
			}
		case stderrors.As(err, &notFound):
			return &querysvc.NotFoundError{
				Message: notFound.Error(),
			}
		case stderrors.As(err, &serviceUnavailable):
			return &querysvc.ServiceUnavailableError{
				Message: serviceUnavailable.Error(),
			}
		default:
			return &querysvc.InternalServerError{
				Message: err.Error(),
			}
		}
	}

	slog.ErrorContext(ctx, "request failed",
		"error", err,
	)
	return f(err)
}
