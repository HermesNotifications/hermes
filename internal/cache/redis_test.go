// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
)

func testRedisURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	return url
}

func TestConnect(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
}

func TestIdempotencyKey_SetNX(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := "test-tenant:test-key-" + time.Now().Format(time.RFC3339Nano)

	// First set should succeed
	existing, err := c.SetIdempotencyKey(ctx, key, "notif-1", time.Hour)
	if err != nil {
		t.Fatalf("SetIdempotencyKey: %v", err)
	}
	if existing != "" {
		t.Fatalf("expected empty (new key), got %s", existing)
	}

	// Second set should return existing
	existing, err = c.SetIdempotencyKey(ctx, key, "notif-2", time.Hour)
	if err != nil {
		t.Fatalf("SetIdempotencyKey: %v", err)
	}
	if existing != "notif-1" {
		t.Fatalf("expected notif-1, got %s", existing)
	}
}

func TestTemplateConfig_Cache(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	slug := "test.template." + time.Now().Format(time.RFC3339Nano)
	data := []byte(`{"slug":"test","email_subject":"Hello"}`)

	// Cache miss
	got, err := c.GetTemplateConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTemplateConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil on cache miss")
	}

	// Set
	if err := c.SetTemplateConfig(ctx, slug, data, 5*time.Minute); err != nil {
		t.Fatalf("SetTemplateConfig: %v", err)
	}

	// Cache hit
	got, err = c.GetTemplateConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTemplateConfig: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %s, got %s", data, got)
	}

	// Invalidate
	if err := c.InvalidateTemplateConfig(ctx, slug); err != nil {
		t.Fatalf("InvalidateTemplateConfig: %v", err)
	}
	got, err = c.GetTemplateConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTemplateConfig after invalidate: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after invalidation")
	}
}
