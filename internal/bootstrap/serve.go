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

	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ServeOptions configures the shutdown sequence. The zero value is what ListenAndServe passes,
// and produces the previous behaviour minus the drain delay.
type ServeOptions struct {
	// Readiness, when set, is flipped to draining on SIGTERM before anything else happens.
	Readiness *httputil.Readiness
	// DrainDelay is how long to keep serving after readiness goes red, giving kube-proxy and
	// the ingress time to stop routing here. Zero uses defaultDrainDelay.
	//
	// It has to happen in-process: every Hermes image is FROM scratch, so there is no shell for
	// a preStop exec hook to run `sleep` in. A cluster on Kubernetes 1.30+ can add
	// preStop.sleep on top, but this is the mechanism that works everywhere.
	DrainDelay time.Duration
	// ShutdownTimeout bounds the graceful HTTP shutdown. Zero uses defaultShutdownTimeout.
	ShutdownTimeout time.Duration
	// OnShutdown run after the drain delay and before the HTTP server stops — releasing work
	// (draining NATS consumers, flushing batches) while the server can still answer.
	OnShutdown []func()
}

const (
	// defaultDrainDelay must exceed one readiness period (5s) plus endpoint propagation. Five
	// seconds is the smallest value that reliably clears both.
	defaultDrainDelay = 5 * time.Second
	// defaultShutdownTimeout replaces a hardcoded 5s that sat under a 30s grace period and so
	// gave up long before it had to.
	defaultShutdownTimeout = 15 * time.Second
)

// ListenAndServe starts the HTTP server, blocks until SIGINT/SIGTERM, then gracefully shuts
// down. Optional onShutdown callbacks run before the HTTP server is stopped (e.g., flushing
// batches, stopping consumers).
//
// Prefer ListenAndServeWithOptions for anything that should drain: without a Readiness this
// cannot flip the probe, so the pod stays in the Service endpoints while it shuts down.
func ListenAndServe(addr string, handler http.Handler, logger *slog.Logger, onShutdown ...func()) {
	ListenAndServeWithOptions(addr, handler, logger, ServeOptions{OnShutdown: onShutdown})
}

// ListenAndServeWithOptions runs the server and the full shutdown sequence.
//
// On SIGTERM, in order:
//
//  1. Flip readiness to draining, so the next probe removes this pod from the Service.
//  2. Keep serving for DrainDelay, while that removal propagates. This is the step that was
//     missing: shutdown used to begin the instant SIGTERM arrived, racing endpoint removal, so
//     every in-flight and newly-routed request in that window was reset.
//  3. Run OnShutdown — stop consuming, finish in-flight work, flush batches.
//  4. Gracefully stop the HTTP server.
//
// OpenTelemetry is initialized on start and flushed on return. When OTEL_EXPORTER_OTLP_ENDPOINT
// is unset, Init is a no-op — safe for local runs without the observability stack.
func ListenAndServeWithOptions(addr string, handler http.Handler, logger *slog.Logger, opts ServeOptions) {
	if code := serve(addr, handler, logger, opts); code != 0 {
		os.Exit(code)
	}
}

// serve is ListenAndServeWithOptions' body, split out so a fatal error can `return 1` and let
// the deferred telemetry flush run. Calling os.Exit inline — which is what the listener
// goroutine used to do — skips every defer, so the traces and metrics explaining the crash were
// discarded precisely when they were most wanted.
func serve(addr string, handler http.Handler, logger *slog.Logger, opts ServeOptions) (exitCode int) {
	ctx := context.Background()

	shutdown, err := observability.Init(ctx, serviceNameFromArgv())
	if err != nil {
		logger.Error("observability init failed", "error", err)
		return 1
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

	// Reported rather than exited on, so a listen failure still unwinds through the OTel flush
	// deferred above. The previous os.Exit(1) here skipped every defer in this function.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("service starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Error("http server error", "error", err)
		return 1
	case <-quit:
	}

	drainDelay := opts.DrainDelay
	if drainDelay == 0 {
		drainDelay = defaultDrainDelay
	}
	shutdownTimeout := opts.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	logger.Info("shutting down", "drain_delay", drainDelay, "shutdown_timeout", shutdownTimeout)

	// Stop advertising as ready, then keep serving while that propagates. Requests still arrive
	// throughout this window — that is the point; they are answered rather than reset.
	if opts.Readiness != nil {
		opts.Readiness.StartDraining()
		time.Sleep(drainDelay)
	}

	for _, fn := range opts.OnShutdown {
		fn()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
	logger.Info("shutdown complete")
	return 0
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
