// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/cmd/service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/log"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/utils"
	"goa.design/clue/debug"
)

// Build-time variables set via ldflags
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

const (
	defaultPort = "8080"
	// gracefulShutdownSeconds should be higher than NATS client
	// request timeout, and lower than the pod or liveness probe's
	// terminationGracePeriodSeconds.
	gracefulShutdownSeconds = 25
)

func init() {
	// slog is the standard library logger, we use it to log errors and
	log.InitStructureLogConfig()
}

func main() {
	// Define command line flags, add any other flag required to configure the
	// service.
	var (
		dbgF = flag.Bool("d", false, "enable debug logging")
		port = flag.String("p", defaultPort, "listen port")
		bind = flag.String("bind", "*", "interface to bind on")
	)
	flag.Usage = func() {
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()

	ctx := context.Background()

	// Set up OpenTelemetry SDK.
	// Command-line/environment OTEL_SERVICE_VERSION takes precedence over
	// the build-time Version variable.
	otelConfig := utils.OTelConfigFromEnv()
	if otelConfig.ServiceVersion == "" {
		otelConfig.ServiceVersion = Version
	}
	otelShutdown, err := utils.SetupOTelSDKWithConfig(ctx, otelConfig)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up OpenTelemetry SDK", "error", err)
		os.Exit(1)
	}
	// Handle shutdown properly so nothing leaks.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownSeconds*time.Second)
		defer cancel()
		if shutdownErr := otelShutdown(ctx); shutdownErr != nil {
			slog.ErrorContext(ctx, "error shutting down OpenTelemetry SDK", "error", shutdownErr)
		}
	}()

	slog.InfoContext(ctx, "Starting query service",
		"bind", *bind,
		"http-port", *port,
		"graceful-shutdown-seconds", gracefulShutdownSeconds,
	)

	// Initialize the resource searcher based on configuration
	resourceSearcher := service.SearcherImpl(ctx)
	accessControlChecker := service.AccessControlCheckerImpl(ctx)
	organizationSearcher := service.OrganizationSearcherImpl(ctx)
	authService := service.AuthServiceImpl(ctx)
	resourceFilter := service.ResourceFilterImpl(ctx)

	// Initialize the services.
	var (
		querySvcSvc querysvc.Service
	)
	{
		querySvcSvc = service.NewQuerySvc(resourceSearcher, accessControlChecker, resourceFilter, organizationSearcher, authService)
	}

	// Wrap the services in endpoints that can be invoked from other services
	// potentially running in different processes.
	querySvcEndpoints := querysvc.NewEndpoints(querySvcSvc)
	querySvcEndpoints.Use(debug.LogPayloads())

	// Create channel used by both the signal handler and server goroutines
	// to notify the main goroutine when to stop the server.
	errc := make(chan error)

	// Setup interrupt handler. This optional step configures the process so
	// that SIGINT and SIGTERM signals cause the services to stop gracefully.
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)

	// Start the servers and send errors (if any) to the error channel.
	addr := ":" + *port
	if *bind != "*" {
		addr = *bind + ":" + *port
	}

	handleHTTPServer(ctx, addr, querySvcEndpoints, &wg, errc, *dbgF)

	// Wait for signal.
	slog.InfoContext(ctx, "received shutdown signal, stopping servers",
		"signal", <-errc,
	)

	// Send cancellation signal to the goroutines.
	cancel()

	// Create a timeout context for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), gracefulShutdownSeconds*time.Second)
	defer shutdownCancel()

	// Gracefully close the access control checker
	go func() {
		if accessControlChecker != nil {
			slog.InfoContext(shutdownCtx, "closing access control checker")
			if err := accessControlChecker.Close(); err != nil {
				slog.ErrorContext(shutdownCtx, "failed to close access control checker", "error", err)
			}
		}
	}()

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.InfoContext(ctx, "graceful shutdown completed")
	case <-shutdownCtx.Done():
		slog.WarnContext(ctx, "graceful shutdown timed out")
	}

	slog.InfoContext(ctx, "exited")
}
