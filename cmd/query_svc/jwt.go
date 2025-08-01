// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package querysvcapi

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
)

const (
	// PS256 is the default for Heimdall's JWT finalizer.
	signatureAlgorithm = validator.PS256
	defaultIssuer      = "heimdall"
	defaultAudience    = "lfx-v2-query-service"
)

var (
	// Factory for custom JWT claims target.
	customClaims = func() validator.CustomClaims {
		return &HeimdallClaims{}
	}
	jwtValidator *validator.Validator
)

// HeimdallClaims contains extra custom claims we want to parse from the JWT
// token.
type HeimdallClaims struct {
	Principal string `json:"principal"`
	Email     string `json:"email,omitempty"`
}

// Validate provides additional middleware validation of any claims defined in
// HeimdallClaims.
func (c *HeimdallClaims) Validate(ctx context.Context) error {
	if c.Principal == "" {
		return errors.New("principal must be provided")
	}
	return nil
}

// SetupJWTAuth initializes the JWT authentication middleware for the service.
func SetupJWTAuth(ctx context.Context) {
	// Set up Heimdall JWKS key provider.
	jwksEnv := os.Getenv("JWKS_URL")
	if jwksEnv == "" {
		jwksEnv = "http://heimdall:4457/.well-known/jwks"
	}
	jwksURL, err := url.Parse(jwksEnv)
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse JWKS URL",
			"url", jwksEnv,
			"error", err,
		)
		log.Fatalf("failed to parse JWKS URL: %v", err)
	}
	var issuer *url.URL
	issuer, err = url.Parse(defaultIssuer)
	if err != nil {
		// This shouldn't happen; a bare hostname is a valid URL.
		slog.ErrorContext(ctx, "failed to parse issuer URL",
			"issuer", defaultIssuer,
			"error", err,
		)
		log.Fatalf("failed to parse issuer URL: %v", err)
	}
	provider := jwks.NewCachingProvider(issuer, 5*time.Minute, jwks.WithCustomJWKSURI(jwksURL))

	// Set up the JWT validator.
	audience := os.Getenv("AUDIENCE")
	if audience == "" {
		audience = defaultAudience
	}
	jwtValidator, err = validator.New(
		provider.KeyFunc,
		signatureAlgorithm,
		issuer.String(),
		[]string{audience},
		validator.WithCustomClaims(customClaims),
		validator.WithAllowedClockSkew(5*time.Second),
	)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create JWT validator",
			"error", err,
		)
		log.Fatalf("failed to create JWT validator: %v", err)
	}
}

// ParsePrincipal extracts the principal from the JWT claims.
func ParsePrincipal(ctx context.Context, token string) (string, error) {

	if mockLocalPrincipal := os.Getenv("JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL"); mockLocalPrincipal != "" {
		slog.InfoContext(ctx, "JWT authentication is disabled, returning mock principal",
			"principal", mockLocalPrincipal,
		)
		return mockLocalPrincipal, nil
	}

	if jwtValidator == nil {
		return "", errors.New("JWT validator is not set up")
	}

	parsedJWT, err := jwtValidator.ValidateToken(ctx, token)
	if err != nil {
		slog.ErrorContext(ctx, "failed to validate JWT token",
			"error", err,
		)
		// Drop tertiary (and deeper) nested errors for security reasons. This is
		// using colons as an approximation for error nesting, which may not
		// exactly match to error boundaries. Unwrapping the error twice, then
		// dropping the suffix of the 3rd error's String() method could be more
		// accurate to error boundaries, but could also expose tertiary errors if
		// errors are not wrapped with Go 1.13 `%w` semantics.
		errString := err.Error()
		firstColon := strings.Index(errString, ":")
		if firstColon != -1 && firstColon+1 < len(errString) {
			errString = strings.Replace(errString, ": go-jose/go-jose/jwt", "", 1)
			secondColon := strings.Index(errString[firstColon+1:], ":")
			if secondColon != -1 {
				// Error has two colons (which may be 3 or more errors), so drop the
				// second colon and everything after it.
				errString = errString[:firstColon+secondColon+1]
			}
		}
		return "", errors.New(errString)
	}

	claims, ok := parsedJWT.(*validator.ValidatedClaims)
	if !ok {
		// This should never happen.
		return "", errors.New("failed to get validated authorization claims")
	}

	customClaims, ok := claims.CustomClaims.(*HeimdallClaims)
	if !ok {
		// This should never happen.
		return "", errors.New("failed to get custom authorization claims")
	}

	return customClaims.Principal, nil
}
