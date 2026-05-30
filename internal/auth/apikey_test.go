// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth_test

import (
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHMACHashAndVerify(t *testing.T) {
	hmacKey := "test-hmac-secret"
	secret := "my-api-key-secret"

	hash := auth.HMACHashAPIKey(secret, hmacKey)
	if hash == "" {
		t.Fatal("hash should not be empty")
	}

	if !auth.HMACVerifyAPIKey(secret, hash, hmacKey) {
		t.Fatal("expected verification to succeed")
	}
}

func TestHMACVerify_WrongSecret(t *testing.T) {
	hmacKey := "test-hmac-secret"
	hash := auth.HMACHashAPIKey("correct-secret", hmacKey)

	if auth.HMACVerifyAPIKey("wrong-secret", hash, hmacKey) {
		t.Fatal("expected verification to fail with wrong secret")
	}
}

func TestHMACVerify_WrongHMACKey(t *testing.T) {
	hash := auth.HMACHashAPIKey("secret", "key1")

	if auth.HMACVerifyAPIKey("secret", hash, "key2") {
		t.Fatal("expected verification to fail with wrong HMAC key")
	}
}

func TestHMACHash_Deterministic(t *testing.T) {
	hmacKey := "test-hmac-secret"
	secret := "my-secret"
	h1 := auth.HMACHashAPIKey(secret, hmacKey)
	h2 := auth.HMACHashAPIKey(secret, hmacKey)

	if h1 != h2 {
		t.Fatalf("HMAC should be deterministic: %s != %s", h1, h2)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(raw, "hms_key_") {
		t.Fatalf("expected hms_key_ prefix, got %s", raw)
	}

	if keyID == "" {
		t.Fatal("keyID should not be empty")
	}

	if !strings.Contains(raw, keyID) {
		t.Fatalf("raw key should contain keyID %s: %s", keyID, raw)
	}
}

func TestGenerateAPIKey_WithEnvPrefix(t *testing.T) {
	raw, _, err := auth.GenerateAPIKey("stg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(raw, "hms_stg_key_") {
		t.Fatalf("expected hms_stg_key_ prefix, got %s", raw)
	}
}

func TestParseAPIKey_Production(t *testing.T) {
	raw, expectedID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatal(err)
	}

	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keyID != expectedID {
		t.Fatalf("expected keyID %s, got %s", expectedID, keyID)
	}
	if secret == "" {
		t.Fatal("secret should not be empty")
	}
}

func TestParseAPIKey_Staging(t *testing.T) {
	raw, expectedID, err := auth.GenerateAPIKey("stg")
	if err != nil {
		t.Fatal(err)
	}

	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keyID != expectedID {
		t.Fatalf("expected keyID %s, got %s", expectedID, keyID)
	}
	if secret == "" {
		t.Fatal("secret should not be empty")
	}
}

func TestParseAPIKey_Invalid(t *testing.T) {
	cases := []string{"", "invalid", "hms_", "hms_key_", "bearer token"}
	for _, c := range cases {
		_, _, err := auth.ParseAPIKey(c)
		if err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestGenerateAndVerify_RoundTrip(t *testing.T) {
	hmacKey := "test-hmac-secret"

	raw, _, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatal(err)
	}

	_, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}

	hash := auth.HMACHashAPIKey(secret, hmacKey)
	if !auth.HMACVerifyAPIKey(secret, hash, hmacKey) {
		t.Fatal("round-trip verification failed")
	}
}
