//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureUser_CreatesOnFirstCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-123")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if user.ExternalID != "ext-123" {
		t.Fatalf("expected external_id ext-123, got %s", user.ExternalID)
	}
	if user.TenantID != tenantID {
		t.Fatalf("expected tenant_id %s, got %s", tenantID, user.TenantID)
	}
}

func TestEnsureUser_ReturnsSameOnSecondCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	first, err := s.EnsureUser(ctx, tenantID, "ext-456")
	if err != nil {
		t.Fatalf("EnsureUser first: %v", err)
	}

	second, err := s.EnsureUser(ctx, tenantID, "ext-456")
	if err != nil {
		t.Fatalf("EnsureUser second: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same ID on second call, got %s and %s", first.ID, second.ID)
	}
}
