// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/linuxfoundation/lfx-v2-query-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NATSClient wraps the NATS connection and provides access control operations
type NATSClient struct {
	conn    *nats.Conn
	config  Config
	timeout time.Duration
}

// NATSClientInterface defines the interface for NATS operations
// This allows for easy mocking and testing
type NATSClientInterface interface {
	CheckAccess(ctx context.Context, request *AccessCheckNATSRequest) (AccessCheckNATSResponse, error)
	ReadTuples(ctx context.Context, request *ReadTuplesNATSRequest) (*ReadTuplesNATSResponse, error)
	Close() error
	IsReady(ctx context.Context) error
}

// requestWithSpan wraps conn.RequestMsgWithContext with an OTel client span and
// injects trace context into the NATS message headers.
func (c *NATSClient) requestWithSpan(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
	ctx, span := tracer.Start(ctx, "nats.request",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", subject),
			attribute.Int("messaging.message.body.size", len(data)),
		),
	)
	defer span.End()

	msg := nats.NewMsg(subject)
	msg.Header = make(nats.Header)
	msg.Data = data
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

	reply, err := c.conn.RequestMsgWithContext(ctx, msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return reply, nil
}

// CheckAccess sends an access control request via NATS and waits for the response
func (c *NATSClient) CheckAccess(ctx context.Context, request *AccessCheckNATSRequest) (AccessCheckNATSResponse, error) {

	if request == nil {
		return nil, fmt.Errorf("invalid NATS access check request: request cannot be nil")
	}

	if request.Subject == "" || request.Message == nil || len(request.Message) == 0 {
		return nil, fmt.Errorf("invalid NATS access check request: subject and message must be set")
	}

	// Apply per-request timeout to context
	timeout := c.timeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Send the request and wait for response
	natsResponse, errRequest := c.requestWithSpan(ctx, request.Subject, request.Message)
	if errRequest != nil {
		return nil, fmt.Errorf("NATS request failed: %w", errRequest)
	}

	slog.DebugContext(ctx, "received NATS response",
		"subject", request.Subject,
		"message", string(natsResponse.Data),
		"timeout", timeout,
	)

	response := make(map[string]string)
	// Deserialize the response
	// Parse the response.
	lines := bytes.Split(natsResponse.Data, []byte("\n"))
	for _, line := range lines {
		// Split the relation from the "allowed" result.
		var relationPart, allowedPart []byte
		var found bool
		if relationPart, allowedPart, found = bytes.Cut(line, []byte("\t")); !found {
			slog.ErrorContext(ctx, "invalid NATS response format",
				"message", string(line),
			)
			return nil, errors.NewUnexpected("invalid NATS response format")
		}
		// Add the response to our map so we can look it up on the corresponding hit.
		response[string(relationPart)] = string(allowedPart)
	}

	return response, nil
}

// ReadTuples sends a read_tuples request via NATS and returns the parsed response
func (c *NATSClient) ReadTuples(ctx context.Context, request *ReadTuplesNATSRequest) (*ReadTuplesNATSResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("invalid NATS read_tuples request: request cannot be nil")
	}
	if request.User == "" || request.ObjectType == "" {
		return nil, fmt.Errorf("invalid NATS read_tuples request: user and object_type are required")
	}

	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal read_tuples request: %w", err)
	}

	timeout := c.timeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	natsResponse, err := c.requestWithSpan(ctx, constants.ReadTuplesSubject, data)
	if err != nil {
		return nil, fmt.Errorf("NATS read_tuples request failed: %w", err)
	}

	slog.DebugContext(ctx, "received NATS read_tuples response",
		"subject", constants.ReadTuplesSubject,
		"message", string(natsResponse.Data),
	)

	var response ReadTuplesNATSResponse
	if err := json.Unmarshal(natsResponse.Data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal read_tuples response: %w", err)
	}

	if response.Error != "" {
		return nil, fmt.Errorf("read_tuples error from fga-sync: %s", response.Error)
	}

	return &response, nil
}

// Close gracefully closes the NATS connection
func (c *NATSClient) Close() error {
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

// IsReady checks if the NATS client is ready
func (c *NATSClient) IsReady(ctx context.Context) error {
	if c.conn == nil {
		return errors.NewServiceUnavailable("NATS client is not initialized or not connected")
	}
	if !c.conn.IsConnected() || c.conn.IsDraining() {
		return errors.NewServiceUnavailable("NATS client is not ready, connection is not established or is draining")
	}
	return nil
}

// NewClient creates a new NATS client with the given configuration
func NewClient(ctx context.Context, config Config) (*NATSClient, error) {
	slog.InfoContext(ctx, "creating NATS client",
		"url", config.URL,
		"timeout", config.Timeout,
	)

	// Validate configuration
	if config.URL == "" {
		return nil, errors.NewUnexpected("NATS URL is required")
	}

	// Configure NATS connection options
	opts := []nats.Option{
		nats.Name(constants.ServiceName),
		nats.Timeout(config.Timeout),
		nats.MaxReconnects(config.MaxReconnect),
		nats.ReconnectWait(config.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			slog.WarnContext(ctx, "NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.InfoContext(ctx, "NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			slog.InfoContext(ctx, "NATS connection closed")
		}),
	}

	// Establish connection
	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, errors.NewServiceUnavailable("failed to connect to NATS", err)
	}

	client := &NATSClient{
		conn:    conn,
		config:  config,
		timeout: config.Timeout,
	}

	slog.InfoContext(ctx, "NATS client created successfully",
		"connected_url", conn.ConnectedUrl(),
		"status", conn.Status(),
	)

	return client, nil
}
