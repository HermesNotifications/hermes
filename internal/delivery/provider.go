// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/models"
)

type DeliveryRequest struct {
	NotificationID string
	OrganizationID string
	UserID         string
	Channel        string
	Title          string
	Body           string
	ActionURL      string
	ActionLabel    string
	EmailTo        string
	PhoneTo        string
	// Metadata is the sender's opaque object. Only the inbox provider does anything with it
	// today: email and SMS have nowhere to put a "toast" hint.
	Metadata models.NotificationMetadata
}

type DeliveryResult struct {
	Success      bool
	ProviderName string
	ProviderID   string
	Error        string
	Metadata     map[string]string
}

type Provider interface {
	Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
	Name() string
}
