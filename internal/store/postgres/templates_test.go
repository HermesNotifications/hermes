//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateTemplate_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email", "inbox"}, "on", 0)

	catID := cat.ID
	subject := "Invoice {{.invoice_number}} paid"
	nt, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		SubscriptionID:  &catID,
		Slug:            "invoice.paid",
		Name:            "Invoice Paid",
		DefaultChannels: []string{"email"},
		EmailSubject:    &subject,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	got, err := s.GetTemplateBySlug(ctx, "invoice.paid")
	if err != nil {
		t.Fatalf("GetTemplateBySlug: %v", err)
	}
	if got.ID != nt.ID {
		t.Fatalf("expected ID %s, got %s", nt.ID, got.ID)
	}
}

func TestDeleteTemplate(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	nt, _ := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug:            "standalone.alert",
		Name:            "Standalone Alert",
		DefaultChannels: []string{},
	})

	if err := s.DeleteTemplate(ctx, nt.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	_, err := s.GetTemplateByID(ctx, nt.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
