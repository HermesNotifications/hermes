package auth_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHashAPIKey_And_Verify(t *testing.T) {
	raw := "hms_test_key_abc123"
	hash, err := auth.HashAPIKey(raw)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}

	if !auth.VerifyAPIKey(raw, hash) {
		t.Fatal("expected key to verify")
	}
}

func TestVerifyAPIKey_WrongKey(t *testing.T) {
	raw := "hms_test_key_abc123"
	hash, err := auth.HashAPIKey(raw)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}

	if auth.VerifyAPIKey("hms_wrong_key", hash) {
		t.Fatal("expected wrong key to not verify")
	}
}

func TestHashAPIKey_DifferentEachTime(t *testing.T) {
	raw := "hms_test_key_abc123"
	h1, _ := auth.HashAPIKey(raw)
	h2, _ := auth.HashAPIKey(raw)
	if h1 == h2 {
		t.Fatal("expected different hashes (different salts)")
	}
}
