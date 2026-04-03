package client

import (
	"context"
	"fmt"
	"net/http"
)

type Content struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
}

type Recipient struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

type SendRequest struct {
	To       Recipient      `json:"to"`
	Template string         `json:"template,omitempty"`
	Content  *Content       `json:"content,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Channels []string       `json:"channels,omitempty"`
}

type SendResponse struct {
	NotificationID string `json:"notification_id"`
}

type NotificationDetail struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	UserID      string   `json:"user_id"`
	CategoryID  string   `json:"category_id"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Channels    []string `json:"channels"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	SentAt      *string  `json:"sent_at,omitempty"`
	DeliveredAt *string  `json:"delivered_at,omitempty"`
	ReadAt      *string  `json:"read_at,omitempty"`
}

type NotificationEvent struct {
	ID             string         `json:"id"`
	NotificationID string         `json:"notification_id"`
	Channel        string         `json:"channel"`
	Event          string         `json:"event"`
	Severity       string         `json:"severity"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

type NotificationStatus struct {
	Notification NotificationDetail `json:"notification"`
	Events       []NotificationEvent `json:"events"`
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
