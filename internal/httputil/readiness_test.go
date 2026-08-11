// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package httputil_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/httputil"
)

func probe(t *testing.T, r *httputil.Readiness) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler()(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec.Code, rec.Body.String()
}

func okCheck(name string) httputil.Check {
	return httputil.Check{Name: name, Fn: func(context.Context) error { return nil }}
}

func failingCheck(name string) httputil.Check {
	return httputil.Check{Name: name, Fn: func(context.Context) error { return errors.New("down") }}
}

func TestReadiness_HealthyIsReady(t *testing.T) {
	r := httputil.NewReadiness(2, okCheck("postgres"))
	if code, _ := probe(t, r); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

// Draining must take effect on the very next probe. The delay that lets in-flight requests
// finish is elsewhere -- here the pod's only job is to stop advertising itself immediately, so
// it leaves the Service endpoints before the HTTP server goes away.
func TestReadiness_DrainingIsImmediate(t *testing.T) {
	r := httputil.NewReadiness(2, okCheck("postgres"))

	r.StartDraining()

	code, body := probe(t, r)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on the first probe after draining began", code)
	}
	if !strings.Contains(body, "draining") {
		t.Fatalf("body = %q, want it to name draining as the reason", body)
	}
	if !r.Draining() {
		t.Fatal("Draining() disagrees with the handler")
	}
}

// A dependency check is debounced; the drain flag is not. One dropped packet to Postgres must
// not eject a healthy pod, but there is nothing transient about a SIGTERM.
func TestReadiness_DependencyFailureIsDebounced(t *testing.T) {
	r := httputil.NewReadiness(2, failingCheck("postgres"))

	if code, _ := probe(t, r); code != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- a single failure is below the threshold", code)
	}
	code, body := probe(t, r)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on the second consecutive failure", code)
	}
	if !strings.Contains(body, "postgres") {
		t.Fatalf("body = %q, want it to name the failed dependency", body)
	}
}

func TestReadiness_RecoveryResetsTheCount(t *testing.T) {
	var fail bool
	r := httputil.NewReadiness(2, httputil.Check{
		Name: "postgres",
		Fn: func(context.Context) error {
			if fail {
				return errors.New("down")
			}
			return nil
		},
	})

	fail = true
	probe(t, r) // one failure, below threshold
	fail = false
	probe(t, r) // recovered
	fail = true

	// Without a reset this would be the second failure and would trip the threshold.
	if code, _ := probe(t, r); code != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- the failure count should have reset on recovery", code)
	}
}

func TestReadiness_NoChecksIsReady(t *testing.T) {
	if code, _ := probe(t, httputil.NewReadiness(2)); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}
