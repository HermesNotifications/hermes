// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package inbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/inbox"
)

// fakeUnreadCache is a hand-written stand-in for the Redis client, typed against
// inbox.UnreadCache. It records the calls the tests care about and lets each one be made to
// fail, which is the only way to reach the degraded branches without an unhealthy Redis.
type fakeUnreadCache struct {
	count int
	ttl   time.Duration
	found bool

	getErr   error
	leaseWon bool
	leaseErr error

	getCalls   int
	fillCalls  int
	leaseCalls int
	setCalls   int
}

func (f *fakeUnreadCache) GetUnreadCountWithTTL(_ context.Context, _ string) (int, time.Duration, bool, error) {
	f.getCalls++
	if f.getErr != nil {
		return 0, 0, false, f.getErr
	}
	return f.count, f.ttl, f.found, nil
}

func (f *fakeUnreadCache) SetUnreadCount(_ context.Context, _ string, count int, _ time.Duration) error {
	f.setCalls++
	f.count, f.found = count, true
	return nil
}

func (f *fakeUnreadCache) FillUnreadCount(_ context.Context, _ string, count int, _ time.Duration) (bool, error) {
	f.fillCalls++
	f.count, f.found = count, true
	return true, nil
}

func (f *fakeUnreadCache) TryUnreadRefreshLease(_ context.Context, _ string, _ time.Duration) (bool, error) {
	f.leaseCalls++
	if f.leaseErr != nil {
		return false, f.leaseErr
	}
	return f.leaseWon, nil
}

func (f *fakeUnreadCache) IncrUnreadCount(_ context.Context, _ string, _ int) (int64, error) {
	return int64(f.count), nil
}

func (f *fakeUnreadCache) DecrUnreadCount(_ context.Context, _ string) (int64, error) {
	return int64(f.count), nil
}

// listUnreadCount drives the count through the real HTTP handler rather than calling the
// unexported helper, so the assertions cover the path a client actually takes.
func listUnreadCount(t *testing.T, srv *inbox.Server) int {
	t.Helper()
	req := requestWithUser(httptest.NewRequest(http.MethodGet, "/v1/inbox", nil), testUserID)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/inbox = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		UnreadCount int `json:"unread_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.UnreadCount
}

// The headline property: a warm cache answers without touching the database. Before this
// change every list ran an uncached COUNT(*) no matter what was cached.
func TestUnreadCount_FreshCacheDoesNotReachTheStore(t *testing.T) {
	srv, store := newTestServer(t)
	fake := &fakeUnreadCache{count: 12, ttl: 10 * time.Minute, found: true}
	srv.SetUnreadCache(fake)

	if got := listUnreadCount(t, srv); got != 12 {
		t.Fatalf("unread_count = %d, want the cached 12", got)
	}
	if store.unreadCountCalls != 0 {
		t.Fatalf("the store was counted %d time(s); a fresh cache hit must not reach it", store.unreadCountCalls)
	}
}

func TestUnreadCount_MissFillsFromStore(t *testing.T) {
	srv, store := newTestServer(t)
	fake := &fakeUnreadCache{found: false}
	srv.SetUnreadCache(fake)

	// The seeded fixture has two unread notifications for the test user.
	if got := listUnreadCount(t, srv); got != 2 {
		t.Fatalf("unread_count = %d, want 2 from the store", got)
	}
	if store.unreadCountCalls != 1 {
		t.Fatalf("store counted %d time(s), want exactly 1", store.unreadCountCalls)
	}
	if fake.fillCalls != 1 {
		t.Fatalf("fill called %d time(s), want exactly 1", fake.fillCalls)
	}
}

// Refresh-ahead is what replaces the accidental self-healing the old list path provided.
func TestUnreadCount_AgingEntryIsRefreshedAhead(t *testing.T) {
	srv, store := newTestServer(t)
	// Well past the refresh threshold, but not yet expired.
	fake := &fakeUnreadCache{count: 99, ttl: time.Minute, found: true, leaseWon: true}
	srv.SetUnreadCache(fake)

	if got := listUnreadCount(t, srv); got != 2 {
		t.Fatalf("unread_count = %d, want the recounted 2 rather than the stale 99", got)
	}
	if store.unreadCountCalls != 1 {
		t.Fatalf("store counted %d time(s), want exactly 1", store.unreadCountCalls)
	}
	if fake.setCalls != 1 {
		t.Fatalf("the refreshed value was written %d time(s), want exactly 1", fake.setCalls)
	}
}

// Losing the lease means another request is already recounting. Serving the slightly stale
// value is the entire point of refreshing ahead of expiry rather than at it -- otherwise a
// burst of requests for one user would each run their own count.
func TestUnreadCount_LosingTheLeaseServesTheStaleValue(t *testing.T) {
	srv, store := newTestServer(t)
	fake := &fakeUnreadCache{count: 99, ttl: time.Minute, found: true, leaseWon: false}
	srv.SetUnreadCache(fake)

	if got := listUnreadCount(t, srv); got != 99 {
		t.Fatalf("unread_count = %d, want the cached 99 while another request recounts", got)
	}
	if store.unreadCountCalls != 0 {
		t.Fatalf("the store was counted %d time(s); the lease holder does that", store.unreadCountCalls)
	}
}

// A failing store during a refresh must not make the answer worse than what was already known.
func TestUnreadCount_RefreshFailureKeepsTheCachedValue(t *testing.T) {
	srv, store := newTestServer(t)
	store.unreadCountErr = errors.New("connection refused")
	fake := &fakeUnreadCache{count: 42, ttl: time.Minute, found: true, leaseWon: true}
	srv.SetUnreadCache(fake)

	if got := listUnreadCount(t, srv); got != 42 {
		t.Fatalf("unread_count = %d, want the cached 42 to survive a failed recount", got)
	}
}

// When Redis itself is failing, fall back to the store but do not write back: a write storm
// aimed at an unhealthy Redis under load makes the situation worse, not better.
func TestUnreadCount_CacheErrorFallsBackWithoutWriting(t *testing.T) {
	srv, store := newTestServer(t)
	fake := &fakeUnreadCache{getErr: errors.New("redis: connection pool timeout")}
	srv.SetUnreadCache(fake)

	if got := listUnreadCount(t, srv); got != 2 {
		t.Fatalf("unread_count = %d, want 2 from the store", got)
	}
	if store.unreadCountCalls != 1 {
		t.Fatalf("store counted %d time(s), want exactly 1", store.unreadCountCalls)
	}
	if fake.fillCalls != 0 || fake.setCalls != 0 {
		t.Fatalf("wrote to a failing cache (%d fills, %d sets); it should be left alone", fake.fillCalls, fake.setCalls)
	}
}

func TestUnreadCount_NoCacheConfiguredUsesTheStore(t *testing.T) {
	srv, store := newTestServer(t)
	srv.SetUnreadCache(nil)

	if got := listUnreadCount(t, srv); got != 2 {
		t.Fatalf("unread_count = %d, want 2 from the store", got)
	}
	if store.unreadCountCalls != 1 {
		t.Fatalf("store counted %d time(s), want exactly 1", store.unreadCountCalls)
	}
}
