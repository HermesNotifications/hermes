// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/hermes-notifications/hermes/internal/httputil"
)

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered",
						"error", err,
						"stack", string(debug.Stack()),
					)
					// Same envelope as every other error this API returns. A
					// panic is the one response a client is least able to guess
					// at, so it should not also be the one that breaks their
					// error parsing.
					httputil.ClientError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
