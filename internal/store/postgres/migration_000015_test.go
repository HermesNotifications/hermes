//go:build integration

// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres_test

import (
	"context"
	"testing"
)

func TestMigration000015_TablesExist(t *testing.T) {
	_, pool := testStore(t)
	ctx := context.Background()

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM template_channel_content`).Scan(&n); err != nil {
		t.Fatalf("template_channel_content not queryable: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_contact_points`).Scan(&n); err != nil {
		t.Fatalf("user_contact_points not queryable: %v", err)
	}
}
