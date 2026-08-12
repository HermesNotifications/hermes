// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"

	"github.com/hermesnotifications/hermes/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// loadseedPermissions are the permissions granted to the single seeded API key.
// Matches cmd/seed/main.go allPermissions minus apikeys:manage (not needed for load runs).
var loadseedPermissions = []string{
	auth.PermNotificationsSend,
	auth.PermTemplatesManage,
	auth.PermOrganizationsManage,
}

// insertAPIKey generates a new API key, writes it to api_keys with the HMAC hash,
// and returns the raw plaintext key (to be stored in the manifest) and key ID.
func insertAPIKey(ctx context.Context, pool *pgxpool.Pool, hmacSecret, name string) (rawKey, keyID string, err error) {
	rawKey, keyID, err = auth.GenerateAPIKey("dev")
	if err != nil {
		return "", "", err
	}
	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		return "", "", err
	}
	hash := auth.HMACHashAPIKey(secret, hmacSecret)
	if _, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4)`,
		keyID, hash, name, loadseedPermissions,
	); err != nil {
		return "", "", err
	}
	return rawKey, keyID, nil
}
