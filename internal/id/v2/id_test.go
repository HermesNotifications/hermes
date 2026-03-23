package id_test

import (
	"encoding/base64"
	"strings"
	"testing"

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

	_, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("suffix is not valid base64url: %v", err)
	}
}

func TestGenerator_New_WithoutPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	got := g.New()

	if strings.Contains(got, "_") {
		t.Fatalf("expected no underscore without prefix, got %s", got)
	}

	_, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("not valid base64url: %v", err)
	}
}

func TestGenerator_New_WithTimeBits(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "ntf", TimeBits: 48, RandBits: 80})
	got := g.New()

	if !strings.HasPrefix(got, "ntf_") {
		t.Fatalf("expected prefix ntf_, got %s", got)
	}

	suffix := strings.TrimPrefix(got, "ntf_")
	raw, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("not valid base64url: %v", err)
	}

	// 48 time bits + 80 random bits = 128 bits = 16 bytes
	if len(raw) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(raw))
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

	prefix, raw := id.Parse(original)
	if prefix != "key" {
		t.Fatalf("expected prefix key, got %s", prefix)
	}
	if len(raw) == 0 {
		t.Fatal("expected raw bytes")
	}
}

func TestParse_NoPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	original := g.New()

	prefix, raw := id.Parse(original)
	if prefix != "" {
		t.Fatalf("expected empty prefix, got %s", prefix)
	}
	if len(raw) == 0 {
		t.Fatal("expected raw bytes")
	}
}
