// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/centrifugo"
	"github.com/hermesnotifications/hermes/internal/models"
)

// unreadCountedTTL bounds how long a notification is remembered as already counted. It only has
// to outlive the redelivery window -- MaxDeliver attempts with backoff capped at 240s -- and an
// hour clears that comfortably without holding a key per notification indefinitely.
const unreadCountedTTL = time.Hour

type InboxProvider struct {
	centrifugo *centrifugo.Client
	cache      *cache.Client
	logger     *slog.Logger
}

func NewInboxProvider(c *centrifugo.Client, cacheClient *cache.Client, logger *slog.Logger) *InboxProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &InboxProvider{centrifugo: c, cache: cacheClient, logger: logger}
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

	// Omitted when empty, like `action` and `unread_count` below, so a send that carries no
	// metadata produces a byte-identical frame to the one clients see today.
	if len(req.Metadata) > 0 {
		payload["metadata"] = req.Metadata
	}

	// Attach the user's unread count to the arrival, so the client does not have to guess it.
	//
	// The increment is guarded on the notification ID because delivery is at-least-once: if the
	// publish below fails, this message is nacked and redelivered, and a second unguarded
	// increment would overcount the user until the entry expires.
	//
	// Claiming the guard before incrementing rather than after is deliberate. Either order has a
	// crash window; this one loses an increment, the other repeats one. An undercount is the
	// better failure: refresh-ahead recomputes from the database within the refresh window,
	// whereas an overcount compounds with every retry.
	//
	// The field is omitted when the cache had no live entry. This process has no database --
	// see cmd/worker-inbox/main.go, which wires NATS, Redis and Centrifugo and nothing else --
	// so on a miss it genuinely does not know the count, and a guess here is how a badge
	// becomes confidently wrong. Clients treat an absent value as "increment locally".
	if p.cache != nil {
		first, err := p.cache.MarkUnreadCounted(ctx, req.NotificationID, unreadCountedTTL)
		if err != nil {
			p.logger.Warn("unread count dedup check failed", "error", err, "notification_id", req.NotificationID)
		} else if first {
			if n, err := p.cache.IncrUnreadCountForArrival(ctx, req.UserID, req.NotificationID, models.UnreadCountCap); err != nil {
				p.logger.Warn("unread count increment failed", "error", err, "user_id", req.UserID)
			} else if n >= 0 {
				payload["unread_count"] = n
			}
		}
	}

	channel := fmt.Sprintf("user#%s", req.UserID)
	if err := p.centrifugo.Publish(ctx, channel, payload); err != nil {
		return DeliveryResult{ProviderName: "inbox", Error: err.Error()}, fmt.Errorf("centrifugo publish: %w", err)
	}
	return DeliveryResult{Success: true, ProviderName: "inbox"}, nil
}
