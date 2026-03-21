package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Content struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
}

type SendRequest struct {
	TenantID string         `json:"tenant_id"`
	UserID   string         `json:"user_id"`
	Type     string         `json:"type,omitempty"`
	Content  *Content       `json:"content,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Channels []string       `json:"channels,omitempty"`
	Group    string         `json:"group,omitempty"`
}

type SendResponse struct {
	NotificationID string `json:"notification_id"`
}

type NotificationStatus struct {
	Notification json.RawMessage `json:"notification"`
	Events       json.RawMessage `json:"events"`
}

type sendOptions struct {
	idempotencyKey string
}

type SendOption func(*sendOptions)

func WithIdempotencyKey(key string) SendOption {
	return func(o *sendOptions) {
		o.idempotencyKey = key
	}
}

type NotificationsService struct{ client *Client }

func (s *NotificationsService) Send(ctx context.Context, body SendRequest, opts ...SendOption) (*SendResponse, error) {
	o := &sendOptions{}
	for _, opt := range opts {
		opt(o)
	}

	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/send", body)
	if err != nil {
		return nil, err
	}

	if o.idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", o.idempotencyKey)
	}

	var resp SendResponse
	if err := s.client.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *NotificationsService) GetStatus(ctx context.Context, id string) (*NotificationStatus, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/notifications/%s", id), nil)
	if err != nil {
		return nil, err
	}

	var status NotificationStatus
	if err := s.client.do(req, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
