// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package httputil

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// checkTimeout bounds a single dependency probe. Readiness runs every 5s with a
// failureThreshold of 1, so a check that takes longer than this has already failed in every
// sense that matters to the kubelet.
const checkTimeout = time.Second

// Check is one named readiness dependency.
type Check struct {
	Name string
	Fn   func(context.Context) error
}

// Readiness answers "will this pod serve correctly right now" — which is a narrower question
// than "is the world healthy", and the difference is load-bearing.
//
// Two rules follow from it, and both were learned from the shape of this system:
//
// A dependency belongs here only when the pod cannot do its job without it. Postgres qualifies
// for the read path; Redis does not, because the cache falls back to the database. Gating
// readiness on Redis would take every replica out of the Service the moment one shared
// dependency blinked, converting a degradation that users would barely notice into a total
// outage. That failure mode is worse than the one it would be protecting against.
//
// A dependency that does fail should not remove the pod on the first blip either, hence the
// consecutive-failure threshold. Draining is exempt from it: a shutting-down pod must leave the
// endpoint list immediately, and there is nothing transient about a SIGTERM.
type Readiness struct {
	checks    []Check
	threshold int

	draining atomic.Bool

	mu     sync.Mutex
	misses map[string]int
}

// NewReadiness builds a readiness probe. A threshold below 1 is treated as 1.
func NewReadiness(threshold int, checks ...Check) *Readiness {
	if threshold < 1 {
		threshold = 1
	}
	return &Readiness{checks: checks, threshold: threshold, misses: map[string]int{}}
}

// StartDraining makes the probe report unready from the next scrape onward.
//
// This is the first thing shutdown does. Until the pod leaves the Service endpoints, kube-proxy
// and the ingress keep routing to it, and anything they send during that window is answered by
// a process that is about to stop listening. Flipping readiness and then *continuing to serve*
// for a few seconds is what converts a rolling restart from "some requests are reset" into "no
// requests are reset".
func (r *Readiness) StartDraining() {
	r.draining.Store(true)
}

// Draining reports whether shutdown has begun.
func (r *Readiness) Draining() bool {
	return r.draining.Load()
}

// Handler serves the readiness probe, naming the failed dependency in the body so an operator
// reading `kubectl describe` learns something more useful than "not ready".
func (r *Readiness) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if r.draining.Load() {
			writeNotReady(w, "draining")
			return
		}
		if failed := r.failing(req.Context()); failed != "" {
			writeNotReady(w, failed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// failing returns the name of the first dependency that has failed enough times in a row to
// count, or "" when the pod is ready.
func (r *Readiness) failing(ctx context.Context) string {
	for _, check := range r.checks {
		checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		err := check.Fn(checkCtx)
		cancel()

		r.mu.Lock()
		if err == nil {
			delete(r.misses, check.Name)
			r.mu.Unlock()
			continue
		}
		r.misses[check.Name]++
		count := r.misses[check.Name]
		r.mu.Unlock()

		if count >= r.threshold {
			return check.Name
		}
	}
	return ""
}

func writeNotReady(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": reason})
}
