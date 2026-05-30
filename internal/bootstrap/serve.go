// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ListenAndServe starts the HTTP server, blocks until SIGINT/SIGTERM,
// then gracefully shuts down. Optional onShutdown callbacks run before
// the HTTP server is stopped (e.g., flushing batches, stopping consumers).
//
// OpenTelemetry is initialized on start and flushed on shutdown. When
// OTEL_EXPORTER_OTLP_ENDPOINT is unset, Init is a no-op — safe for local
// runs without the observability stack.
func ListenAndServe(addr string, handler http.Handler, logger *slog.Logger, onShutdown ...func()) {
	ctx := context.Background()

	shutdown, err := observability.Init(ctx, serviceNameFromArgv())
	if err != nil {
		logger.Error("observability init failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(flushCtx); err != nil {
			logger.Error("observability shutdown error", "error", err)
		}
	}()

	// Wrap the final handler with otelhttp so every request becomes a server
	// span named by http.route. Health checks get filtered so they don't
	// dominate the trace store.
	instrumented := otelhttp.NewHandler(handler, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
		}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	server := &http.Server{
		Addr:    addr,
		Handler: instrumented,
	}

	go func() {
		logger.Info("service starting", "addr", addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	for _, fn := range onShutdown {
		fn()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
}

// serviceNameFromArgv derives the OTel service.name from the binary name.
// Overridden by OTEL_SERVICE_NAME or OTEL_RESOURCE_ATTRIBUTES env vars,
// which is how deployment manifests set the canonical hermes-<svc> name.
func serviceNameFromArgv() string {
	if len(os.Args) == 0 {
		return "hermes-unknown"
	}
	name := filepath.Base(os.Args[0])
	if name == "" || name == "." || name == "/" {
		return "hermes-unknown"
	}
	return "hermes-" + name
}
