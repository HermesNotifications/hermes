package auth

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type mockRedis struct {
	mu   sync.Mutex
	data []byte
	ttl  time.Duration
}

func (m *mockRedis) GetJWTSigningKeys(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, nil
	}
	cp := make([]byte, len(m.data))
	copy(cp, m.data)
	return cp, nil
}

func (m *mockRedis) SetJWTSigningKeys(_ context.Context, data []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make([]byte, len(data))
	copy(m.data, data)
	m.ttl = ttl
	return nil
}

func testKeys() []JWTSigningConfig {
	return []JWTSigningConfig{
		{Name: "key1", Secret: []byte("secret1"), Algorithm: "HS256", UserIDClaim: "sub", TenantIDClaim: "tenant_id"},
	}
}

func TestCachedKeys_InProcessHit(t *testing.T) {
	calls := 0
	upstream := func() []JWTSigningConfig {
		calls++
		return testKeys()
	}

	ck := NewCachedKeyProvider(upstream, time.Minute, nil)
	provider := ck.Provider()

	// First call hits upstream.
	keys := provider()
	if len(keys) != 1 || keys[0].Name != "key1" {
		t.Fatalf("expected key1, got %v", keys)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}

	// Second call should be in-process hit.
	keys = provider()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if calls != 1 {
		t.Fatalf("expected still 1 upstream call, got %d", calls)
	}
}

func TestCachedKeys_RedisHitOnInProcessMiss(t *testing.T) {
	upstreamCalls := 0
	upstream := func() []JWTSigningConfig {
		upstreamCalls++
		return testKeys()
	}

	redis := &mockRedis{}

	// Pre-populate Redis with cached keys.
	data, _ := json.Marshal(testKeys())
	redis.SetJWTSigningKeys(context.Background(), data, 10*time.Minute)

	// Use a very short TTL so in-process expires immediately.
	ck := NewCachedKeyProvider(upstream, time.Nanosecond, redis)
	provider := ck.Provider()

	// Allow in-process to expire.
	time.Sleep(time.Millisecond)

	keys := provider()
	if len(keys) != 1 || keys[0].Name != "key1" {
		t.Fatalf("expected key1 from Redis, got %v", keys)
	}
	// Upstream (DB) should not have been called because Redis had data.
	if upstreamCalls != 0 {
		t.Fatalf("expected 0 upstream calls, got %d", upstreamCalls)
	}
}

func TestCachedKeys_FullMissQueries_Upstream(t *testing.T) {
	upstreamCalls := 0
	upstream := func() []JWTSigningConfig {
		upstreamCalls++
		return testKeys()
	}

	redis := &mockRedis{} // empty Redis

	ck := NewCachedKeyProvider(upstream, time.Nanosecond, redis)
	provider := ck.Provider()

	keys := provider()
	if len(keys) != 1 || keys[0].Name != "key1" {
		t.Fatalf("expected key1 from upstream, got %v", keys)
	}
	if upstreamCalls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstreamCalls)
	}

	// Redis should now be populated.
	data, err := redis.GetJWTSigningKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected Redis to be populated")
	}
}

func TestCachedKeys_NilRedis_FallsThrough(t *testing.T) {
	upstreamCalls := 0
	upstream := func() []JWTSigningConfig {
		upstreamCalls++
		return testKeys()
	}

	ck := NewCachedKeyProvider(upstream, time.Nanosecond, nil)
	provider := ck.Provider()

	// First call.
	provider()
	if upstreamCalls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstreamCalls)
	}

	// In-process expired, no Redis → upstream again.
	time.Sleep(time.Millisecond)
	provider()
	if upstreamCalls != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", upstreamCalls)
	}
}
