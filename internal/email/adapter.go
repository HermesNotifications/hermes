// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package email

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/hermes-notifications/hermes/internal/delivery"
)

// DeliveryAdapter wraps an email Provider to satisfy the delivery.Provider interface.
type DeliveryAdapter struct {
	provider Provider
	from     string
	layout   *template.Template
}

// NewDeliveryAdapter creates a new adapter that bridges email.Provider to delivery.Provider.
func NewDeliveryAdapter(provider Provider, from string, layout *template.Template) *DeliveryAdapter {
	return &DeliveryAdapter{
		provider: provider,
		from:     from,
		layout:   layout,
	}
}

func (a *DeliveryAdapter) Name() string {
	return a.provider.Name()
}

func (a *DeliveryAdapter) Send(ctx context.Context, req delivery.DeliveryRequest) (delivery.DeliveryResult, error) {
	if req.EmailTo == "" {
		return delivery.DeliveryResult{
			Success:      false,
			ProviderName: a.provider.Name(),
			Error:        "no recipient email address",
		}, fmt.Errorf("no recipient email address")
	}

	htmlBody := req.Body
	if a.layout != nil {
		var buf bytes.Buffer
		if err := a.layout.Execute(&buf, map[string]string{"Content": htmlBody, "Title": req.Title}); err != nil {
			return delivery.DeliveryResult{}, fmt.Errorf("execute layout: %w", err)
		}
		htmlBody = buf.String()
	}

	email := Email{
		From:     a.from,
		To:       req.EmailTo,
		Subject:  req.Title,
		HTMLBody: htmlBody,
	}

	providerID, err := a.provider.Send(ctx, email)
	if err != nil {
		return delivery.DeliveryResult{
			Success:      false,
			ProviderName: a.provider.Name(),
			Error:        err.Error(),
		}, err
	}

	return delivery.DeliveryResult{
		Success:      true,
		ProviderName: a.provider.Name(),
		ProviderID:   providerID,
	}, nil
}
