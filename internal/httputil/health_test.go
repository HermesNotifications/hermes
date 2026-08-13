// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package httputil_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/httputil"
)

func live(t *testing.T, checks ...httputil.Check) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	httputil.HealthzHandler(checks...)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return rec.Code, rec.Body.String()
}

// The historical behaviour, still what every service that consumes nothing gets: no checks, always
// alive. Restarting an HTTP service because a dependency is down helps nobody.
func TestHealthz_NoChecksIsAlwaysAlive(t *testing.T) {
	if code, _ := live(t); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

func TestHealthz_PassingCheckIsAlive(t *testing.T) {
	code, _ := live(t, okCheck("consumer-progress"))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

// A failed liveness check restarts the container, so the response has to say which check failed
// and why — the kubelet only records "probe failed", and by the time anyone looks, the process
// that could have explained itself has been replaced.
func TestHealthz_FailingCheckIsNamedInTheBody(t *testing.T) {
	code, body := live(t, okCheck("first"), httputil.Check{
		Name: "consumer-progress",
		Fn:   func(context.Context) error { return errors.New("dispatch has taken nothing from a backlog of 133472") },
	})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", code)
	}
	for _, want := range []string{"consumer-progress", "133472"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q does not mention %q", body, want)
		}
	}
}

// A check that hangs must not hang the probe: the kubelet would time out and call it a failure
// anyway, and a restart decision should be made by the check rather than by a stuck goroutine.
func TestHealthz_CheckIsBounded(t *testing.T) {
	blocked := httputil.Check{
		Name: "slow",
		Fn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan int, 1)
	go func() {
		code, _ := live(t, blocked)
		done <- code
	}()

	select {
	case code := <-done:
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 from a check that could not answer in time", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the probe did not return; the check is not bounded")
	}
}
