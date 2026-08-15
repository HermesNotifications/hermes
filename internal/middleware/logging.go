// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// SlowRequestThreshold promotes an otherwise-uninteresting request to Warn once
// it takes at least this long. Tail latency is the one property of a successful
// request worth a log line of its own: it is rare by construction, so it cannot
// flood, and it is the thing traces are least likely to have sampled.
const SlowRequestThreshold = 1 * time.Second

// Logging emits a structured record per request, at a level chosen by outcome.
// It uses the ...Context variants so the logger's observability.TraceHandler
// picks up trace_id/span_id from the request context — no explicit correlation
// needed here.
//
// The level is not Info for everything, which is what it used to be. One record
// per request means log volume tracks traffic, and at production rates the
// successful ones are both the overwhelming majority and the least informative:
// the same ground is covered by the request span in Tempo and by the RED metrics
// off it. So a plain 2xx sits at Debug, where an operator can switch it back on
// for one service (HERMES_LOG_LEVEL=debug) without every service paying for it,
// and the levels above are reserved for requests that actually went wrong:
//
//	5xx   Error   the service failed
//	4xx   Warn    the caller was refused — auth, validation, rate limit
//	slow  Warn    succeeded, but over SlowRequestThreshold
//	else  Debug   the steady state
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				return
			}

			elapsed := time.Since(start)
			level := slog.LevelDebug
			switch {
			case rec.status >= 500:
				level = slog.LevelError
			case rec.status >= 400:
				level = slog.LevelWarn
			case elapsed >= SlowRequestThreshold:
				level = slog.LevelWarn
			}

			// Guarded so the attribute slice is not built for the Debug records
			// that this middleware produces on nearly every request.
			if !logger.Enabled(r.Context(), level) {
				return
			}
			logger.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}
