// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler wraps another slog.Handler and injects trace_id / span_id
// from the record's context whenever a span is active. Other records pass
// through unchanged.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler wraps inner with trace-context injection.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

// WrapJSONHandler wraps a stdout slog.JSONHandler with trace correlation.
// This is the common case — services call this in bootstrap.NewLogger.
func WrapJSONHandler(inner slog.Handler) slog.Handler {
	return NewTraceHandler(inner)
}

// Enabled delegates to the wrapped handler.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle adds trace_id and span_id attributes when the record's context
// carries an active span, then delegates.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new handler that also adds attrs to every record.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new handler that places subsequent attrs under name.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}
