// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package models_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestStatusRank(t *testing.T) {
	tests := []struct {
		status models.NotificationStatus
		rank   int
	}{
		{models.StatusPending, 0},
		{models.StatusSent, 1},
		{models.StatusDelivered, 2},
		{models.StatusRead, 3},
		{models.StatusArchived, 4},
	}
	for _, tt := range tests {
		if got := tt.status.Rank(); got != tt.rank {
			t.Errorf("StatusRank(%s) = %d, want %d", tt.status, got, tt.rank)
		}
	}
}

func TestStatusRank_CannotRegress(t *testing.T) {
	current := models.StatusDelivered
	incoming := models.StatusSent
	if incoming.Rank() >= current.Rank() {
		t.Fatal("sent should not be >= delivered")
	}
}
