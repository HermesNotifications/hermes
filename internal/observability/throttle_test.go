// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func throttleLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func decodeAll(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

// The first occurrence is always admitted: an operator should learn that a
// dependency went sick when it goes sick, not one interval later.
func TestLogThrottle_AdmitsFirstImmediately(t *testing.T) {
	tr := NewLogThrottle(time.Hour)
	logger, buf := throttleLogger()

	tr.Log(context.Background(), logger, slog.LevelWarn, "redis unavailable", "error", "dial tcp")

	recs := decodeAll(t, buf)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0]["msg"] != "redis unavailable" {
		t.Errorf("msg = %v", recs[0]["msg"])
	}
	// Nothing was suppressed before the first, so the attribute must be absent
	// rather than present-and-zero.
	if _, ok := recs[0]["suppressed"]; ok {
		t.Errorf("first record should carry no suppressed attr, got %v", recs[0]["suppressed"])
	}
}

// The whole point: a per-request call site under a sustained outage costs one
// record, not one per request.
func TestLogThrottle_SuppressesWithinInterval(t *testing.T) {
	tr := NewLogThrottle(time.Hour)
	logger, buf := throttleLogger()

	for range 10_000 {
		tr.Log(context.Background(), logger, slog.LevelWarn, "redis unavailable")
	}

	if n := len(decodeAll(t, buf)); n != 1 {
		t.Errorf("expected 1 record from 10000 calls, got %d", n)
	}
}

// Suppression must be reported, otherwise the surviving record understates the
// problem — one line looks like one blip.
func TestLogThrottle_ReportsSuppressedCount(t *testing.T) {
	tr := NewLogThrottle(time.Hour)
	logger, buf := throttleLogger()
	ctx := context.Background()

	tr.Log(ctx, logger, slog.LevelWarn, "redis unavailable") // admitted
	for range 42 {
		tr.Log(ctx, logger, slog.LevelWarn, "redis unavailable") // suppressed
	}

	// Force the interval to have elapsed rather than sleeping for it.
	tr.mu.Lock()
	tr.last = tr.last.Add(-2 * time.Hour)
	tr.mu.Unlock()

	tr.Log(ctx, logger, slog.LevelWarn, "redis unavailable") // admitted again

	recs := decodeAll(t, buf)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if got := recs[1]["suppressed"]; got != float64(42) {
		t.Errorf("suppressed = %v, want 42", got)
	}

	// The counter resets, so the next admitted record does not re-report the
	// same suppressions.
	tr.mu.Lock()
	tr.last = tr.last.Add(-2 * time.Hour)
	tr.mu.Unlock()
	tr.Log(ctx, logger, slog.LevelWarn, "redis unavailable")

	recs = decodeAll(t, buf)
	if _, ok := recs[2]["suppressed"]; ok {
		t.Errorf("expected no suppressed attr after reset, got %v", recs[2]["suppressed"])
	}
}

// These sit on per-request paths, so they are called concurrently by definition.
// Run with -race.
func TestLogThrottle_ConcurrentCallersSafe(t *testing.T) {
	tr := NewLogThrottle(time.Hour)
	logger, buf := throttleLogger()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				tr.Log(context.Background(), logger, slog.LevelWarn, "redis unavailable")
			}
		}()
	}
	wg.Wait()

	if n := len(decodeAll(t, buf)); n != 1 {
		t.Errorf("expected 1 record from 10000 concurrent calls, got %d", n)
	}
}

// A nil logger is the zero value of the field on RateLimiter when WithShared was
// never called, so the throttle has to tolerate it.
func TestLogThrottle_NilLoggerIsNoOp(t *testing.T) {
	tr := NewLogThrottle(time.Hour)
	tr.Log(context.Background(), nil, slog.LevelWarn, "redis unavailable")
}
