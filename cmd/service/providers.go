// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/auth"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/clearbit"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/filter"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/mock"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/nats"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/opensearch"
	"github.com/linuxfoundation/lfx-v2-query-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
)

// ResourceSearchConfigImpl reads the count-walk and access-check tunables
// from the environment. Every variable has a safe default so an unconfigured
// deployment behaves like the defaults in pkg/constants.
//
//   - ACCESS_CHECK_TIMEOUT   (default 15s)   timeout of each batched access check
//   - READ_TUPLES_TIMEOUT    (default 15s)   timeout of the filter_grants=direct tuple read
//   - COUNT_ACCESS_BUCKET_PAGE (default 100) access-key buckets fetched and checked per page
//   - COUNT_MAX_ACCESS_BUCKETS (default 5000) buckets walked before a count reports has_more
func ResourceSearchConfigImpl(ctx context.Context) service.Config {
	config := service.DefaultConfig()

	config.AccessCheckTimeout = envDuration("ACCESS_CHECK_TIMEOUT", config.AccessCheckTimeout)
	config.ReadTuplesTimeout = envDuration("READ_TUPLES_TIMEOUT", config.ReadTuplesTimeout)
	config.AccessBucketPage = envInt("COUNT_ACCESS_BUCKET_PAGE", constants.DefaultAccessBucketPage)
	config.MaxAccessBuckets = envInt("COUNT_MAX_ACCESS_BUCKETS", constants.DefaultMaxAccessBuckets)

	if err := config.Validate(); err != nil {
		log.Fatalf("invalid resource search configuration: %v", err)
	}

	slog.InfoContext(ctx, "resource search configuration",
		"access_check_timeout", config.AccessCheckTimeout,
		"read_tuples_timeout", config.ReadTuplesTimeout,
		"count_access_bucket_page", config.AccessBucketPage,
		"count_max_access_buckets", config.MaxAccessBuckets,
	)
	return config
}

// envDuration reads a duration from the environment, falling back to def
// when unset and fatally rejecting an unparsable value.
func envDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		log.Fatalf("invalid %s duration %q: %v", name, raw, err)
	}
	return value
}

// envInt reads an integer from the environment, falling back to def when
// unset and fatally rejecting an unparsable value.
func envInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid %s value %q: %v", name, raw, err)
	}
	return value
}

// AuthServiceImpl initializes the authentication service implementation
func AuthServiceImpl(ctx context.Context) port.Authenticator {
	var authService port.Authenticator

	// Repository implementation configuration
	authSource := os.Getenv("AUTH_SOURCE")
	if authSource == "" {
		authSource = "jwt"
	}

	switch authSource {
	case "mock":
		slog.InfoContext(ctx, "initializing mock authentication service")
		authService = mock.NewMockAuthService()
	case "jwt":
		slog.InfoContext(ctx, "initializing JWT authentication service")
		jwtConfig := auth.JWTAuthConfig{
			JWKSURL:  os.Getenv("JWKS_URL"),
			Audience: os.Getenv("JWT_AUDIENCE"),
		}
		jwtAuth, err := auth.NewJWTAuth(jwtConfig)
		if err != nil {
			log.Fatalf("failed to initialize JWT authentication service: %v", err)
		}
		authService = jwtAuth
	default:
		log.Fatalf("unsupported authentication service implementation: %s", authSource)
	}

	return authService
}

// SearcherImpl injects the resource searcher implementation
func SearcherImpl(ctx context.Context) port.ResourceSearcher {

	var (
		resourceSearcher port.ResourceSearcher
		err              error
	)

	// Search source implementation configuration
	searchSource := os.Getenv("SEARCH_SOURCE")
	if searchSource == "" {
		searchSource = "opensearch"
	}

	opensearchURL := os.Getenv("OPENSEARCH_URL")
	if opensearchURL == "" {
		opensearchURL = "http://localhost:9200"
	}

	opensearchIndex := os.Getenv("OPENSEARCH_INDEX")
	if opensearchIndex == "" {
		opensearchIndex = "resources"
	}

	switch searchSource {
	case "mock":
		slog.InfoContext(ctx, "initializing mock resource searcher")
		resourceSearcher = mock.NewMockResourceSearcher()

	case "opensearch":
		slog.InfoContext(ctx, "initializing opensearch resource searcher",
			"url", opensearchURL,
			"index", opensearchIndex,
		)
		opensearchConfig := opensearch.Config{
			URL:   opensearchURL,
			Index: opensearchIndex,
		}

		resourceSearcher, err = opensearch.NewSearcher(ctx, opensearchConfig)
		if err != nil {
			log.Fatalf("failed to initialize OpenSearch searcher: %v", err)
		}

	default:
		log.Fatalf("unsupported search implementation: %s", searchSource)
	}

	return resourceSearcher

}

// AccessControlCheckerImpl injects the access control checker implementation
func AccessControlCheckerImpl(ctx context.Context) port.AccessControlChecker {

	var (
		accessControlChecker port.AccessControlChecker
		err                  error
	)

	// Access control implementation configuration
	accessControlSource := os.Getenv("ACCESS_CONTROL_SOURCE")
	if accessControlSource == "" {
		accessControlSource = "nats"
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	natsTimeout := os.Getenv("NATS_TIMEOUT")
	if natsTimeout == "" {
		natsTimeout = "10s"
	}
	natsTimeoutDuration, err := time.ParseDuration(natsTimeout)
	if err != nil {
		log.Fatalf("invalid NATS timeout duration: %v", err)
	}

	natsMaxReconnect := os.Getenv("NATS_MAX_RECONNECT")
	if natsMaxReconnect == "" {
		natsMaxReconnect = "3"
	}
	natsMaxReconnectInt, err := strconv.Atoi(natsMaxReconnect)
	if err != nil {
		log.Fatalf("invalid NATS max reconnect value %s: %v", natsMaxReconnect, err)
	}

	natsReconnectWait := os.Getenv("NATS_RECONNECT_WAIT")
	if natsReconnectWait == "" {
		natsReconnectWait = "2s"
	}
	natsReconnectWaitDuration, err := time.ParseDuration(natsReconnectWait)
	if err != nil {
		log.Fatalf("invalid NATS reconnect wait duration %s : %v", natsReconnectWait, err)
	}

	// Initialize the access control checker based on configuration
	switch accessControlSource {
	case "mock":
		slog.InfoContext(ctx, "initializing mock access control checker")
		accessControlChecker = mock.NewMockAccessControlChecker()

	case "nats":
		slog.InfoContext(ctx, "initializing NATS access control checker")
		natsConfig := nats.Config{
			URL:           natsURL,
			Timeout:       natsTimeoutDuration,
			MaxReconnect:  natsMaxReconnectInt,
			ReconnectWait: natsReconnectWaitDuration,
		}

		accessControlChecker, err = nats.NewAccessControlChecker(ctx, natsConfig)
		if err != nil {
			log.Fatalf("failed to initialize NATS access control checker: %v", err)
		}

	default:
		log.Fatalf("unsupported access control implementation: %s", accessControlSource)
	}

	return accessControlChecker
}

// OrganizationSearcherImpl injects the organization searcher implementation
func OrganizationSearcherImpl(ctx context.Context) port.OrganizationSearcher {

	var (
		organizationSearcher port.OrganizationSearcher
		err                  error
	)

	// Organization search source implementation configuration
	orgSearchSource := os.Getenv("ORG_SEARCH_SOURCE")
	if orgSearchSource == "" {
		orgSearchSource = "clearbit"
	}

	switch orgSearchSource {
	case "mock":
		slog.InfoContext(ctx, "initializing mock organization searcher")
		organizationSearcher = mock.NewMockOrganizationSearcher()

	case "clearbit":
		// Parse Clearbit environment variables
		clearbitAPIKey := os.Getenv("CLEARBIT_CREDENTIAL")
		clearbitBaseURL := os.Getenv("CLEARBIT_BASE_URL")
		clearbitAutocompleteBaseURL := os.Getenv("CLEARBIT_AUTOCOMPLETE_BASE_URL")
		clearbitTimeout := os.Getenv("CLEARBIT_TIMEOUT")

		clearbitMaxRetries := os.Getenv("CLEARBIT_MAX_RETRIES")
		clearbitMaxRetriesInt := 3 // default
		if clearbitMaxRetries != "" {
			clearbitMaxRetriesInt, err = strconv.Atoi(clearbitMaxRetries)
			if err != nil {
				log.Fatalf("invalid Clearbit max retries value %s: %v", clearbitMaxRetries, err)
			}
		}

		clearbitRetryDelay := os.Getenv("CLEARBIT_RETRY_DELAY")

		clearbitConfig, err := clearbit.NewConfig(clearbitAPIKey,
			clearbitBaseURL,
			clearbitAutocompleteBaseURL,
			clearbitTimeout,
			clearbitMaxRetriesInt,
			clearbitRetryDelay,
		)
		if err != nil {
			log.Fatalf("failed to create Clearbit configuration: %v", err)
		}

		slog.InfoContext(ctx, "initializing Clearbit organization searcher",
			"base_url", clearbitConfig.BaseURL,
			"autocomplete_base_url", clearbitConfig.AutocompleteBaseURL,
			"timeout", clearbitConfig.Timeout,
			"max_retries", clearbitConfig.MaxRetries,
		)

		organizationSearcher, err = clearbit.NewOrganizationSearcher(ctx, clearbitConfig)
		if err != nil {
			log.Fatalf("failed to initialize Clearbit organization searcher: %v", err)
		}

	default:
		log.Fatalf("unsupported organization search implementation: %s", orgSearchSource)
	}

	return organizationSearcher
}

// ResourceFilterImpl injects the resource filter implementation
func ResourceFilterImpl(ctx context.Context) port.ResourceFilter {
	slog.InfoContext(ctx, "initializing CEL resource filter")

	celFilter, err := filter.NewCELFilter()
	if err != nil {
		log.Fatalf("failed to initialize CEL filter: %v", err)
	}

	return celFilter
}
