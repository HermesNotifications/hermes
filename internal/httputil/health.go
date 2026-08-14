// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package httputil

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// HealthzHandler returns the liveness handler: 200 unless one of the given checks fails, and with
// no checks it always answers 200.
//
// Liveness answers "should this container be restarted", which is a much narrower question than
// readiness and demands a much higher bar. A dependency being down is never an answer to it —
// restarting the process does not fix Postgres, and a check that said otherwise would restart
// every pod in the fleet the moment one shared dependency blinked. The only thing that belongs
// here is state the process itself has got into and cannot get out of, where a restart is the
// remedy. There is exactly one such check today: messaging's consumer stall detector, which is
// here because a wedged JetStream consumer left the write path stopped for three hours behind a
// /healthz that was hardcoded to 200. See internal/messaging/stall.go.
//
// Each check runs with the same bound readiness uses; a check that cannot answer in a second has
// failed in every sense the kubelet cares about. The failing check is named in the body so the
// probe failure in `kubectl describe` can be traced to a cause.
func HealthzHandler(checks ...Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, check := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
			err := check.Fn(ctx)
			cancel()
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status": "unhealthy",
					"check":  check.Name,
					"reason": err.Error(),
				})
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
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
