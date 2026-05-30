// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
)

type InboxProvider struct {
	centrifugo *centrifugo.Client
	cache      *cache.Client
}

func NewInboxProvider(c *centrifugo.Client, cacheClient *cache.Client) *InboxProvider {
	return &InboxProvider{centrifugo: c, cache: cacheClient}
}

func (p *InboxProvider) Name() string { return "inbox" }

func (p *InboxProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	payload := map[string]any{
		"type":       "notification.new",
		"id":         req.NotificationID,
		"title":      req.Title,
		"body":       req.Body,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"timestamp":  time.Now().UnixMilli(),
	}
	if req.ActionURL != "" || req.ActionLabel != "" {
		action := map[string]string{}
		if req.ActionURL != "" {
			action["url"] = req.ActionURL
		}
		if req.ActionLabel != "" {
			action["label"] = req.ActionLabel
		}
		payload["action"] = action
	}

	// Increment cached unread count for the user
	if p.cache != nil {
		if _, err := p.cache.IncrUnreadCount(ctx, req.UserID); err != nil {
			_ = err // Non-fatal: cache will self-correct on next ListInbox
		}
	}

	channel := fmt.Sprintf("user#%s", req.UserID)
	if err := p.centrifugo.Publish(ctx, channel, payload); err != nil {
		return DeliveryResult{ProviderName: "inbox", Error: err.Error()}, fmt.Errorf("centrifugo publish: %w", err)
	}
	return DeliveryResult{Success: true, ProviderName: "inbox"}, nil
}
