//go:build integration

package dynamo_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testClient creates a DynamoDB client pointed at DynamoDB Local (or the endpoint
// set by HERMES_DYNAMO_ENDPOINT). Tables are created if they don't already exist.
// The default of localhost:8000 matches the docker-compose dynamo-local service.
func testClient(t *testing.T) *dynamo.Client {
	t.Helper()
	endpoint := os.Getenv("HERMES_DYNAMO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}
	region := os.Getenv("HERMES_DYNAMO_REGION")
	if region == "" {
		region = "us-east-1"
	}
	client, err := dynamo.NewClient(context.Background(), endpoint, region)
	if err != nil {
		t.Fatalf("dynamo.NewClient: %v", err)
	}
	if err := client.EnsureTables(context.Background()); err != nil {
		t.Fatalf("EnsureTables: %v", err)
	}
	return client
}

// testPGStore creates a Postgres store for tests that require the delegation path
// (UpdateNotificationStatus, BatchUpdateNotificationStatuses). Runs migrations and
// registers a cleanup to close the pool.
func testPGStore(t *testing.T) (*postgres.Store, *pgxpool.Pool) {
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

// cleanPGTables truncates Postgres tables for test isolation.
func cleanPGTables(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		if _, err := pool.Exec(context.Background(), "TRUNCATE "+tbl+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}

// seedNotification creates the minimal Postgres records (tenant → user → category →
// notification) required by tests that exercise the Postgres delegation path.
func seedNotification(t *testing.T, st *postgres.Store, notifID string) {
	t.Helper()
	ctx := context.Background()
	tenantID := uuid.New().String()
	if _, err := st.CreateTenant(ctx, tenantID, "dynamo-test-tenant"); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := st.EnsureUser(ctx, tenantID, "dynamo-test-user-"+uuid.New().String())
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	cat, err := st.CreateCategory(ctx, "dynamo-test-cat-"+uuid.New().String(), "Dynamo Test", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	n := &models.Notification{
		ID:         notifID,
		TenantID:   tenantID,
		UserID:     user.ID,
		CategoryID: cat.ID,
		Title:      "dynamo integration test",
		Body:       "body",
		Channels:   []string{"inbox"},
		Status:     models.StatusPending,
	}
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
}
