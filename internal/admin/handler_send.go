package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

type sendRequest struct {
	TenantID string         `json:"tenant_id"`
	UserID   string         `json:"user_id"`
	Type     string         `json:"type,omitempty"`
	Content  *content       `json:"content,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Channels []string       `json:"channels,omitempty"`
	Group    string         `json:"group,omitempty"`
}

type content struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
}

type sendResponse struct {
	NotificationID string `json:"notification_id"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate: exactly one of type or content
	if (req.Type == "" && req.Content == nil) || (req.Type != "" && req.Content != nil) {
		s.clientError(w, http.StatusBadRequest, "exactly one of 'type' or 'content' must be provided")
		return
	}
	if req.TenantID == "" || req.UserID == "" {
		s.clientError(w, http.StatusBadRequest, "tenant_id and user_id are required")
		return
	}

	ctx := r.Context()

	// Validate tenant exists
	if _, err := s.store.GetTenantByID(ctx, req.TenantID); err != nil {
		s.clientError(w, http.StatusBadRequest, "unknown tenant_id")
		return
	}

	// Generate notification ID upfront (needed for idempotency key)
	notifID := id.New()

	// Check idempotency key
	idemKey := r.Header.Get("X-Idempotency-Key")
	if idemKey != "" && s.cache != nil {
		// Try Redis SetNX with the real notification ID
		existing, err := s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, notifID, time.Hour)
		if err == nil && existing != "" {
			s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: existing})
			return
		}
		// On Redis error, fall back to Postgres
		if err != nil {
			n, dbErr := s.store.GetNotificationByIdempotencyKey(ctx, req.TenantID, idemKey)
			if dbErr == nil && n != nil {
				s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, n.ID, time.Hour)
				s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: n.ID})
				return
			}
		}
	}

	// Resolve group
	var groupID string
	var typeID *string
	if req.Type != "" {
		nt, err := s.store.GetTypeBySlug(ctx, req.Type)
		if err != nil {
			s.clientError(w, http.StatusBadRequest, "unknown notification type")
			return
		}
		groupID = nt.GroupID
		typeID = &nt.ID
	} else {
		if req.Group == "" {
			s.clientError(w, http.StatusBadRequest, "group is required for direct sends")
			return
		}
		g, err := s.store.GetGroupBySlug(ctx, req.Group)
		if err != nil {
			s.clientError(w, http.StatusBadRequest, "unknown group")
			return
		}
		groupID = g.ID
	}

	// Ensure user exists
	user, err := s.store.EnsureUser(ctx, req.TenantID, req.UserID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Build notification
	n := &models.Notification{
		ID:       notifID,
		TenantID: req.TenantID,
		UserID:   user.ID,
		TypeID:   typeID,
		GroupID:  groupID,
		Channels: req.Channels,
		Status:   models.StatusPending,
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
	persisted, err := s.store.CreateNotification(ctx, n)
	if err != nil {
		s.serverError(w, err)
		return
	}
	notifID = persisted.ID

	// Publish to NATS
	if s.nats != nil {
		msg := map[string]any{
			"notification_id": notifID,
			"tenant_id":       req.TenantID,
			"user_id":         user.ID,
			"content": map[string]any{
				"title":        n.Title,
				"body":         n.Body,
				"action_url":   n.ActionURL,
				"action_label": n.ActionLabel,
			},
			"metadata": map[string]any{
				"group": req.Group,
				"type":  req.Type,
			},
			"data":    req.Data,
			"attempt": 1,
		}
		if len(req.Channels) > 0 {
			msg["channels"] = req.Channels
		}

		msgBytes, _ := json.Marshal(msg)
		if err := s.nats.Publish("notification.send", msgBytes); err != nil {
			s.logger.Error("failed to publish to NATS", "error", err, "notification_id", notifID)
		}
	}

	s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: notifID})
}
