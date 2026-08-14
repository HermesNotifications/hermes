// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package bootstrap

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

// StartDebugServer serves net/http/pprof on its own port, when one is configured.
//
// On a separate port, not the service's HTTP port, because send, inbox and user answer public
// traffic there and /debug/pprof is both an information leak (full command line, every
// goroutine stack) and a cheap denial of service (a goroutine dump under load is not free).
// Nothing in deploy/ or charts/ puts this port in a Service, so reaching it takes a
// deliberate `kubectl port-forward`.
//
// blockRate and mutexFraction are arguments rather than an internal default because they are
// the profiles that matter for this system and both are off unless asked for. Everything
// measured at scale says Hermes waits rather than computes -- 11-24% node CPU while holding
// 250,000 connections, the inbox service at 50m against a 256Mi limit -- and a CPU profile of
// a process that is parked in pgx waiting on commit shows an idle process. Only the block and
// mutex profiles record waiting.
//
// The server is deliberately not wired into the graceful-shutdown sequence in serve.go: it
// holds no client state worth draining, and a debug listener that can refuse connections
// during shutdown is a debug listener that is missing when the interesting thing happens.
func StartDebugServer(port, blockRate, mutexFraction int, logger *slog.Logger) {
	if port <= 0 {
		return
	}

	// SetBlockProfileRate samples one blocking event per this many nanoseconds spent blocked;
	// 1 records everything and is genuinely expensive. SetMutexProfileFraction samples 1 in N
	// contention events. Both perturb what they measure, so profile a dedicated run rather
	// than the run whose numbers you intend to quote.
	if blockRate > 0 {
		runtime.SetBlockProfileRate(blockRate)
	}
	if mutexFraction > 0 {
		runtime.SetMutexProfileFraction(mutexFraction)
	}

	mux := http.NewServeMux()
	// pprof.Index dispatches the named profiles under /debug/pprof/ itself --
	// heap, goroutine, block, mutex, allocs, threadcreate. The other four are separate
	// handlers because they are not runtime/pprof profiles.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
		// No WriteTimeout on purpose: /debug/pprof/profile?seconds=30 and the runtime trace
		// are long-lived by design, and a write deadline truncates them into a corrupt
		// profile rather than failing cleanly. ReadHeaderTimeout still bounds a slowloris.
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("debug server listening",
			"addr", addr, "block_profile_rate", blockRate, "mutex_profile_fraction", mutexFraction)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Logged, never fatal. A profiling endpoint that fails to bind must not take
			// the service down with it.
			logger.Error("debug server stopped", "error", err)
		}
	}()
}
