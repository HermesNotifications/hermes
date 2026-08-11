// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package httputil

import (
	"context"
	"net/http"
	"time"
)

// HealthzHandler returns a handler that always responds 200 OK.
func HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

// ReadyzHandler returns a handler that runs all check functions before responding.
// If any check fails, it responds 503 Service Unavailable.
// If no checks are provided, it always responds 200 OK.
func ReadyzHandler(checks ...func(ctx context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		for _, check := range checks {
			if err := check(ctx); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}
