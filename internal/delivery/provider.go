// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import "context"

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
