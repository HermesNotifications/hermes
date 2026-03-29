package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

type sendContent struct {
	Title       string `json:"title" doc:"Notification title"`
	Body        string `json:"body" doc:"Notification body"`
	ActionURL   string `json:"action_url,omitempty" doc:"Optional action URL"`
	ActionLabel string `json:"action_label,omitempty" doc:"Optional action button label"`
}

type sendInput struct {
	IdempotencyKey string `header:"X-Idempotency-Key" required:"false" doc:"Idempotency key for deduplication"`
	Body           struct {
		TenantID string         `json:"tenant_id" required:"true" minLength:"1" doc:"Tenant identifier"`
		UserID   string         `json:"user_id" required:"true" minLength:"1" doc:"External user identifier"`
		Template string         `json:"template,omitempty" doc:"Notification template slug (mutually exclusive with content)"`
		Content  *sendContent   `json:"content,omitempty" doc:"Direct content (mutually exclusive with template)"`
		Data     map[string]any `json:"data,omitempty" doc:"Template data for rendering"`
		Channels []string       `json:"channels,omitempty" doc:"Explicit delivery channels"`
	}
}

type sendOutput struct {
	Body struct {
		NotificationID string `json:"notification_id" doc:"ID of the created notification"`
	}
}

func (s *Server) registerSendRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID:   "send-notification",
		Method:        http.MethodPost,
		Path:          "/v1/send",
		Summary:       "Send a notification",
		Tags:          []string{"Notifications"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, input *sendInput) (*sendOutput, error) {
		req := &input.Body

		// Validate: exactly one of template or content
		if (req.Template == "" && req.Content == nil) || (req.Template != "" && req.Content != nil) {
			return nil, huma.Error400BadRequest("exactly one of 'template' or 'content' must be provided")
		}

		// Validate tenant exists
		if _, err := s.store.GetTenantByID(ctx, req.TenantID); err != nil {
			return nil, huma.Error400BadRequest("unknown tenant_id")
		}

		// Generate notification ID upfront (needed for idempotency key)
		notifID := id.New()

		// Check idempotency key
		idemKey := input.IdempotencyKey
		if idemKey != "" && s.cache != nil {
			existing, err := s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, notifID, time.Hour)
			if err == nil && existing != "" {
				resp := &sendOutput{}
				resp.Body.NotificationID = existing
				return resp, nil
			}
			if err != nil {
				n, dbErr := s.store.GetNotificationByIdempotencyKey(ctx, req.TenantID, idemKey)
				if dbErr == nil && n != nil {
					s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, n.ID, time.Hour)
					resp := &sendOutput{}
					resp.Body.NotificationID = n.ID
					return resp, nil
				}
			}
		}

		// Resolve category from template's subscription, or require channels for direct sends
		var categoryID string
		var templateID *string
		var subscriptionID string
		if req.Template != "" {
			nt, err := s.store.GetTemplateBySlug(ctx, req.Template)
			if err != nil {
				return nil, huma.Error400BadRequest("unknown notification template")
			}
			templateID = &nt.ID
			if nt.SubscriptionID != nil {
				sub, err := s.store.GetSubscriptionByID(ctx, *nt.SubscriptionID)
				if err != nil {
					return nil, huma.Error500InternalServerError("internal server error")
				}
				categoryID = sub.CategoryID
				subscriptionID = sub.ID
			}
		} else {
			if len(req.Channels) == 0 {
				return nil, huma.Error400BadRequest("channels required for direct content sends")
			}
		}

		// Ensure user exists
		user, err := s.store.EnsureUser(ctx, req.TenantID, req.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build notification
		channels := req.Channels
		if channels == nil {
			channels = []string{}
		}
		n := &models.Notification{
			ID:         notifID,
			TenantID:   req.TenantID,
			UserID:     user.ID,
			TemplateID: templateID,
			CategoryID: categoryID,
			Channels:   channels,
			Status:     models.StatusPending,
		}

		if req.Content != nil {
			n.Title = req.Content.Title
			n.Body = req.Content.Body
			if req.Content.ActionURL != "" {
				n.ActionURL = &req.Content.ActionURL
			}
			if req.Content.ActionLabel != "" {
				n.ActionLabel = &req.Content.ActionLabel
			}
		}

		if idemKey != "" {
			n.IdempotencyKey = &idemKey
		}

		// Persist
		if _, err := s.store.CreateNotification(ctx, n); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Publish to NATS
		if s.nats != nil {
			msg := map[string]any{
				"notification_id":  notifID,
				"tenant_id":        req.TenantID,
				"user_id":          user.ID,
				"content": map[string]any{
					"title":        n.Title,
					"body":         n.Body,
					"action_url":   n.ActionURL,
					"action_label": n.ActionLabel,
				},
				"metadata": map[string]any{
					"template": req.Template,
				},
				"category_id":     categoryID,
				"subscription_id": subscriptionID,
				"data":            req.Data,
				"attempt":         1,
			}
			if len(req.Channels) > 0 {
				msg["channels"] = req.Channels
			}

			msgBytes, _ := json.Marshal(msg)
			if err := s.nats.Publish(ctx, "notification.send", msgBytes); err != nil {
				s.logger.Error("failed to publish to NATS", "error", err, "notification_id", notifID)
			}
		}

		resp := &sendOutput{}
		resp.Body.NotificationID = notifID
		return resp, nil
	})
}
