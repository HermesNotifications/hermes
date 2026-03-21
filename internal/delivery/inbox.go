package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/centrifugo"
)

type InboxProvider struct {
	centrifugo *centrifugo.Client
}

func NewInboxProvider(c *centrifugo.Client) *InboxProvider {
	return &InboxProvider{centrifugo: c}
}

func (p *InboxProvider) Name() string { return "inbox" }

func (p *InboxProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	payload := map[string]any{
		"id":         req.NotificationID,
		"title":      req.Title,
		"body":       req.Body,
		"created_at": time.Now().UTC().Format(time.RFC3339),
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

	channel := fmt.Sprintf("user#%s", req.UserID)
	if err := p.centrifugo.Publish(ctx, channel, payload); err != nil {
		return DeliveryResult{ProviderName: "inbox", Error: err.Error()}, fmt.Errorf("centrifugo publish: %w", err)
	}
	return DeliveryResult{Success: true, ProviderName: "inbox"}, nil
}
