// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package config_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		t.Fatal("expected default DatabaseURL")
	}
	if cfg.NATSUrl == "" {
		t.Fatal("expected default NATSUrl")
	}
	if cfg.RedisURL == "" {
		t.Fatal("expected default RedisURL")
	}
	if cfg.HTTPPort == 0 {
		t.Fatal("expected default HTTPPort")
	}
	if cfg.DispatchConcurrency != 4 {
		t.Fatalf("expected default DispatchConcurrency 4, got %d", cfg.DispatchConcurrency)
	}
	if cfg.DispatchPrefetch != 64 {
		t.Fatalf("expected default DispatchPrefetch 64, got %d", cfg.DispatchPrefetch)
	}
}

func TestLoad_DispatchConcurrencyOverride(t *testing.T) {
	t.Setenv("HERMES_DISPATCH_CONCURRENCY", "8")

	cfg := config.Load()
	if cfg.DispatchConcurrency != 8 {
		t.Fatalf("expected DispatchConcurrency 8, got %d", cfg.DispatchConcurrency)
	}
}

func TestLoad_OverrideFromEnv(t *testing.T) {
	t.Setenv("HERMES_HTTP_PORT", "9999")
	t.Setenv("HERMES_DATABASE_URL", "postgres://custom:5432/hermes")

	cfg := config.Load()
	if cfg.HTTPPort != 9999 {
		t.Fatalf("expected port 9999, got %d", cfg.HTTPPort)
	}
	if cfg.DatabaseURL != "postgres://custom:5432/hermes" {
		t.Fatalf("expected custom DB URL, got %s", cfg.DatabaseURL)
	}
}
