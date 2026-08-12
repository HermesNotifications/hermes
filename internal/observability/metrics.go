// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Meter returns a named Meter from the global MeterProvider. Use the
// canonical import path of the calling package as the name so metrics can
// be traced back to their origin.
//
//	var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/send")
func Meter(name string) metric.Meter {
	return otel.Meter(name)
}

// Tracer returns a named Tracer from the global TracerProvider.
//
//	var tracer = observability.Tracer("github.com/hermesnotifications/hermes/internal/send")
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
