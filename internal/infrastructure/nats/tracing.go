// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// tracer is safe to initialize at package level — otel.Tracer() returns a
// delegating tracer that forwards to whatever TracerProvider is registered at
// call time, so otel.SetTracerProvider() updates it regardless of init order.
var tracer = otel.Tracer("github.com/linuxfoundation/lfx-v2-query-service/internal/infrastructure/nats")

// natsHeaderCarrier adapts nats.Header to the OTel TextMapCarrier interface
// so trace context can be injected into NATS message headers.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string {
	// nats.Header canonicalizes keys to MIME-style casing (e.g., "Traceparent").
	// Use Header.Get to ensure consistent key canonicalization with downstream code.
	return nats.Header(c).Get(key)
}

func (c natsHeaderCarrier) Set(key string, value string) {
	// Use Header.Set to ensure keys are canonicalized (e.g., "traceparent" -> "Traceparent").
	// This ensures trace context injection is readable by code that uses Header.Get.
	nats.Header(c).Set(key, value)
}

func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

var _ propagation.TextMapCarrier = natsHeaderCarrier{}
