//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCreateTenant_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	tenant, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	got, err := s.GetTenantByID(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenantByID: %v", err)
	}
	if got.Name != "Test Tenant" {
		t.Fatalf("expected Test Tenant, got %s", got.Name)
	}
}
