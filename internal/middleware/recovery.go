// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/hermesnotifications/hermes/internal/httputil"
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
