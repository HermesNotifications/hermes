//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) (*postgres.Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("HERMES_DATABASE_URL")
	if url == "" {
		url = "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"
	}
	if err := database.RunMigrations(url, "../../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	pool, err := database.NewPool(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return postgres.New(pool), pool
}

// cleanTable truncates a table for test isolation
func cleanTable(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		_, err := pool.Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
		if err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}
