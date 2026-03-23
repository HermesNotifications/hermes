//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateType_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_types", "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email", "inbox"})

	subject := "Invoice {{.invoice_number}} paid"
	nt, err := s.CreateType(ctx, &models.NotificationType{
		GroupID:      g.ID,
		Slug:         "invoice.paid",
		Name:         "Invoice Paid",
		EmailSubject: &subject,
	})
	if err != nil {
		t.Fatalf("CreateType: %v", err)
	}

	got, err := s.GetTypeBySlug(ctx, "invoice.paid")
	if err != nil {
		t.Fatalf("GetTypeBySlug: %v", err)
	}
	if got.ID != nt.ID {
		t.Fatalf("expected ID %s, got %s", nt.ID, got.ID)
	}
}

func TestDeleteType(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_types", "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email"})
	nt, _ := s.CreateType(ctx, &models.NotificationType{
		GroupID: g.ID, Slug: "invoice.paid", Name: "Invoice Paid",
	})

	if err := s.DeleteType(ctx, nt.ID); err != nil {
		t.Fatalf("DeleteType: %v", err)
	}

	_, err := s.GetTypeByID(ctx, nt.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
