package id_test

import (
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
)

func TestNew_Returns26CharString(t *testing.T) {
	got := id.New()
	if len(got) != 26 {
		t.Fatalf("expected 26 chars, got %d: %q", len(got), got)
	}
}

func TestNew_IsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := id.New()
		if seen[v] {
			t.Fatalf("duplicate ID: %s", v)
		}
		seen[v] = true
	}
}

func TestNew_IsSortable(t *testing.T) {
	a := id.New()
	time.Sleep(2 * time.Millisecond)
	b := id.New()
	if a >= b {
		t.Fatalf("expected %s < %s", a, b)
	}
}

func TestNew_UsesValidCrockfordChars(t *testing.T) {
	const charset = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	valid := make(map[byte]bool)
	for i := 0; i < len(charset); i++ {
		valid[charset[i]] = true
	}
	for i := 0; i < 100; i++ {
		v := id.New()
		for j := 0; j < len(v); j++ {
			if !valid[v[j]] {
				t.Fatalf("invalid char %c in ID %s", v[j], v)
			}
		}
	}
}
