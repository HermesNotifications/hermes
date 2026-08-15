// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// capture returns a logger writing JSON to buf at the given level, matching how
// bootstrap.NewLogger builds the real one.
func capture(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})), buf
}

// records parses the buffer as a stream of JSON log records.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log record: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

// handlerReturning responds with status after sleeping for delay.
func handlerReturning(status int, delay time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
	})
}

// The point of the middleware after this change: level is a function of outcome,
// so a healthy service at any request rate emits nothing above Debug.
func TestLogging_LevelFollowsOutcome(t *testing.T) {
	tests := []struct {
		name   string
		status int
		delay  time.Duration
		want   string
	}{
		{"success is debug", http.StatusOK, 0, "DEBUG"},
		{"redirect is debug", http.StatusFound, 0, "DEBUG"},
		{"client error is warn", http.StatusUnauthorized, 0, "WARN"},
		{"rate limited is warn", http.StatusTooManyRequests, 0, "WARN"},
		{"server error is error", http.StatusInternalServerError, 0, "ERROR"},
		{"slow success is warn", http.StatusOK, SlowRequestThreshold + 20*time.Millisecond, "WARN"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := capture(slog.LevelDebug)
			h := Logging(logger)(handlerReturning(tc.status, tc.delay))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/inbox", nil))

			recs := records(t, buf)
			if len(recs) != 1 {
				t.Fatalf("expected 1 record, got %d", len(recs))
			}
			if got := recs[0]["level"]; got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
			if got := recs[0]["msg"]; got != "request" {
				t.Errorf("msg = %v, want \"request\"", got)
			}
			if got := recs[0]["status"]; got != float64(tc.status) {
				t.Errorf("status = %v, want %d", got, tc.status)
			}
		})
	}
}

// The volume claim, stated as a test: at the production default level, successful
// requests cost nothing at all. If someone re-promotes the success path to Info,
// this fails.
func TestLogging_SuccessSilentAtInfo(t *testing.T) {
	logger, buf := capture(slog.LevelInfo)
	h := Logging(logger)(handlerReturning(http.StatusOK, 0))

	for range 100 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/inbox", nil))
	}

	if n := len(records(t, buf)); n != 0 {
		t.Errorf("expected no records for 100 successful requests, got %d", n)
	}
}

// Failures must survive the quieting — turning the level down is not allowed to
// hide the requests an operator turned it down to find.
func TestLogging_FailuresSurviveAtInfo(t *testing.T) {
	logger, buf := capture(slog.LevelInfo)
	h := Logging(logger)(handlerReturning(http.StatusInternalServerError, 0))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/send", nil))

	recs := records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", recs[0]["level"])
	}
	if recs[0]["method"] != http.MethodPost {
		t.Errorf("method = %v, want POST", recs[0]["method"])
	}
}

// Health endpoints were already exempt; the rewrite must not have reintroduced them.
func TestLogging_SkipsHealthEndpoints(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		logger, buf := capture(slog.LevelDebug)
		h := Logging(logger)(handlerReturning(http.StatusOK, 0))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

		if n := len(records(t, buf)); n != 0 {
			t.Errorf("%s: expected no record, got %d", path, n)
		}
	}
}
