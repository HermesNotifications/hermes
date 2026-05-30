// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package main

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertAPIKey(t *testing.T) {
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	ctx := context.Background()
	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	hmac := envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret")
	raw, keyID, err := insertAPIKey(ctx, pool, hmac, "loadtest-"+os.Getenv("HOSTNAME"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, keyID) }()

	id, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != keyID {
		t.Fatalf("id mismatch: %s != %s", id, keyID)
	}

	var hash string
	if err := pool.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id = $1`, keyID).Scan(&hash); err != nil {
		t.Fatalf("query hash: %v", err)
	}
	if !auth.HMACVerifyAPIKey(secret, hash, hmac) {
		t.Fatalf("key does not verify")
	}
}
