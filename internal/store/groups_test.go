//go:build integration

package store_test

import (
	"context"
	"testing"
)

func TestCreateGroup_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	g, err := s.CreateGroup(ctx, "billing", "Billing", []string{"email", "inbox"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Slug != "billing" {
		t.Fatalf("expected slug billing, got %s", g.Slug)
	}

	got, err := s.GetGroupBySlug(ctx, "billing")
	if err != nil {
		t.Fatalf("GetGroupBySlug: %v", err)
	}
	if got.ID != g.ID {
		t.Fatalf("expected ID %s, got %s", g.ID, got.ID)
	}
}

func TestListGroups(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	s.CreateGroup(ctx, "billing", "Billing", []string{"email"})
	s.CreateGroup(ctx, "security", "Security", []string{"email", "sms"})

	groups, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestUpdateGroup(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email"})

	updated, err := s.UpdateGroup(ctx, g.ID, "Billing Notifications", []string{"email", "inbox"})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if updated.Name != "Billing Notifications" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
	if len(updated.DefaultChannels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(updated.DefaultChannels))
	}
}
