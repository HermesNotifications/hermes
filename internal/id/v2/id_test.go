// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package id_test

import (
	"strings"
	"testing"
	"time"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
)

func TestGenerator_New_WithPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	got := g.New()

	if !strings.HasPrefix(got, "key_") {
		t.Fatalf("expected prefix key_, got %s", got)
	}

	suffix := strings.TrimPrefix(got, "key_")
	if len(suffix) == 0 {
		t.Fatal("suffix should not be empty")
	}

	// All chars should be from the base62 alphabet
	for _, c := range suffix {
		if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", c) {
			t.Fatalf("unexpected character %c in ID %s", c, got)
		}
	}
}

func TestGenerator_New_WithoutPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	got := g.New()

	for _, c := range got {
		if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", c) {
			t.Fatalf("unexpected character %c in ID %s", c, got)
		}
	}
}

func TestGenerator_New_FixedWidth(t *testing.T) {
	g := id.NewGenerator(id.Config{TimeBits: 48, RandBits: 80})
	first := g.New()
	width := len(first)

	for i := 0; i < 100; i++ {
		got := g.New()
		if len(got) != width {
			t.Fatalf("expected width %d, got %d for ID %s", width, len(got), got)
		}
	}
}

func TestGenerator_New_LexicographicTimeOrdering(t *testing.T) {
	g := id.NewGenerator(id.Config{TimeBits: 48, RandBits: 80})

	a := g.New()
	time.Sleep(2 * time.Millisecond)
	b := g.New()

	if a >= b {
		t.Fatalf("expected %s < %s (lexicographic time ordering)", a, b)
	}
}

func TestGenerator_New_Uniqueness(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := g.New()
		if seen[v] {
			t.Fatalf("duplicate ID: %s", v)
		}
		seen[v] = true
	}
}

func TestParse(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	original := g.New()

	prefix, value := id.Parse(original)
	if prefix != "key" {
		t.Fatalf("expected prefix key, got %s", prefix)
	}
	if len(value) == 0 {
		t.Fatal("expected non-empty value")
	}
}

func TestParse_NoPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	original := g.New()

	prefix, value := id.Parse(original)
	if prefix != "" {
		t.Fatalf("expected empty prefix, got %s", prefix)
	}
	if value != original {
		t.Fatalf("expected value to equal original %s, got %s", original, value)
	}
}

func TestDecodeBase62_Roundtrip(t *testing.T) {
	g := id.NewGenerator(id.Config{TimeBits: 48, RandBits: 80})
	original := g.New()

	decoded := id.DecodeBase62(original)
	if len(decoded) == 0 {
		t.Fatal("expected non-empty decoded bytes")
	}
}

func TestNotificationGenerator(t *testing.T) {
	got := id.Notification.New()

	// No prefix
	if strings.Contains(got, "_") {
		t.Fatalf("notification ID should not have a prefix: %s", got)
	}

	// Should be 22 chars (128 bits in base62)
	if len(got) != 22 {
		t.Fatalf("expected 22 chars, got %d: %s", len(got), got)
	}
}

func TestUserGenerator(t *testing.T) {
	got := id.User.New()

	if !strings.HasPrefix(got, "usr_") {
		t.Fatalf("expected usr_ prefix, got %s", got)
	}
}
