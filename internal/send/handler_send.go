// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package send

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermes-notifications/hermes/internal/send")

// publishRejectionCounter is the signal that the pipeline is refusing work.
// Before the work streams were bounded this could only mean NATS was
// unreachable; it now also means a stream hit its ceiling, which is the
// condition an operator needs to see before callers start reporting 503s.
var publishRejectionCounter, _ = meter.Int64Counter(
	"hermes.send.publish_rejections",
	metric.WithDescription("Send requests refused because the notification could not be published to NATS."),
	metric.WithUnit("1"),
)

type sendRecipient struct {
	OrganizationID string            `json:"organization_id" required:"true" minLength:"1" doc:"Organization identifier"`
	UserID         string            `json:"user_id" required:"true" minLength:"1" doc:"External user identifier"`
	Contacts       map[string]string `json:"contacts,omitempty" doc:"Per-channel address overrides: address key (\"email\",\"phone\") -> address"`
}

type sendContent struct {
	Title       string `json:"title" doc:"Notification title"`
	Body        string `json:"body" doc:"Notification body"`
	ActionURL   string `json:"action_url,omitempty" doc:"Optional action URL"`
	ActionLabel string `json:"action_label,omitempty" doc:"Optional action button label"`
}

type sendInput struct {
	IdempotencyKey string `header:"X-Idempotency-Key" required:"false" doc:"Idempotency key for deduplication"`
	Body           struct {
		To       sendRecipient  `json:"to" required:"true" doc:"Notification recipient"`
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
		// Finding 3. /v1/send is the platform's primary write path and previously
		// accepted ANY valid API key: a key issued narrowly for template management
		// could forge notifications to any user in any organization.
		if err := requirePermission(ctx, auth.PermNotificationsSend); err != nil {
			return nil, err
		}

		req := &input.Body

		// Validate: exactly one of template or content
		if (req.Template == "" && req.Content == nil) || (req.Template != "" && req.Content != nil) {
			return nil, huma.Error400BadRequest("exactly one of 'template' or 'content' must be provided")
		}

		// Validate: direct sends require channels
		if req.Content != nil && len(req.Channels) == 0 {
			return nil, huma.Error400BadRequest("channels required for direct content sends")
		}

		// Generate notification ID
		notifID := id.Notification.New()

		// Idempotency check via Redis SET NX
		idemKey := input.IdempotencyKey
		if idemKey != "" && s.cache != nil {
			existing, err := s.cache.SetIdempotencyKey(ctx, req.To.OrganizationID+":"+idemKey, notifID, time.Hour)
			if err == nil && existing != "" {
				resp := &sendOutput{}
				resp.Body.NotificationID = existing
				return resp, nil
			}
		}

		// Build SendMessage
		msg := &hermenats.SendMessage{
			NotificationID: notifID,
			OrganizationID: req.To.OrganizationID,
			ExternalUserID: req.To.UserID,
			Contacts:       req.To.Contacts,
			Metadata: hermenats.MessageMetadata{
				Template: req.Template,
			},
			Data:           req.Data,
			Channels:       req.Channels,
			IdempotencyKey: idemKey,
			Attempt:        1,
		}

		if req.Content != nil {
			mc := &hermenats.MessageContent{
				Title: req.Content.Title,
				Body:  req.Content.Body,
			}
			if req.Content.ActionURL != "" {
				mc.ActionURL = &req.Content.ActionURL
			}
			if req.Content.ActionLabel != "" {
				mc.ActionLabel = &req.Content.ActionLabel
			}
			msg.Content = mc
		}

		// Publish to NATS
		msgBytes, err := msg.Marshal()
		if err != nil {
			s.logger.Error("failed to marshal send message", "error", err, "notification_id", notifID)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if s.nats == nil {
			s.logger.Error("NATS client not configured", "notification_id", notifID)
			return nil, huma.NewError(http.StatusServiceUnavailable, "service temporarily unavailable")
		}

		if err := s.nats.Publish(ctx, "notification.send", msgBytes); err != nil {
			s.logger.Error("failed to publish to NATS", "error", err, "notification_id", notifID)
			publishRejectionCounter.Add(ctx, 1)
			// Now that the work streams are bounded with DiscardNew, a publish can
			// fail because the pipeline is saturated rather than because NATS is
			// unreachable. Both are "we could not accept this, try later", so the
			// status stays 503 — a 429 would blame the caller for a backlog that a
			// stalled consumer, not their request rate, most likely caused.
			//
			// What was missing is the "try later" half: a 503 with no Retry-After
			// leaves a client to invent a backoff, and the ones that invent badly
			// retry hardest exactly when the pipeline is already behind.
			return nil, huma.ErrorWithHeaders(
				huma.NewError(http.StatusServiceUnavailable, "service temporarily unavailable"),
				http.Header{"Retry-After": []string{"5"}},
			)
		}

		resp := &sendOutput{}
		resp.Body.NotificationID = notifID
		return resp, nil
	})
}
