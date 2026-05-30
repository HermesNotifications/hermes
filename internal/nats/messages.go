// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package hermenats

import "encoding/json"

// SendMessage is published to notification.send by the Send service.
type SendMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	ExternalUserID string          `json:"external_user_id"`
	Email          string          `json:"email,omitempty"`
	Phone          string          `json:"phone,omitempty"`
	Content        *MessageContent `json:"content,omitempty"`
	Metadata       MessageMetadata `json:"metadata"`
	Data           map[string]any  `json:"data,omitempty"`
	Channels       []string        `json:"channels,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Attempt        int             `json:"attempt"`
}

type MessageContent struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	ActionURL   *string `json:"action_url,omitempty"`
	ActionLabel *string `json:"action_label,omitempty"`
}

type MessageMetadata struct {
	Template string `json:"template,omitempty"`
}

// Recipient holds resolved contact information for the notification target.
type Recipient struct {
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// DeliveryMessage is published to delivery.{channel} by the Dispatch service.
type DeliveryMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	UserID         string          `json:"user_id"`
	Channel        string          `json:"channel"`
	Content        MessageContent  `json:"content"`
	Metadata       MessageMetadata `json:"metadata"`
	Recipient      Recipient       `json:"recipient"`
	Attempt        int             `json:"attempt"`
}

// EventMessage is published to notification.events by Dispatch and Workers.
type EventMessage struct {
	NotificationID string         `json:"notification_id"`
	Channel        string         `json:"channel"`
	Event          string         `json:"event"`
	Severity       string         `json:"severity"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (m *SendMessage) Marshal() ([]byte, error)     { return json.Marshal(m) }
func (m *DeliveryMessage) Marshal() ([]byte, error) { return json.Marshal(m) }
func (m *EventMessage) Marshal() ([]byte, error)    { return json.Marshal(m) }

func UnmarshalSend(data []byte) (*SendMessage, error) {
	var m SendMessage
	return &m, json.Unmarshal(data, &m)
}

func UnmarshalDelivery(data []byte) (*DeliveryMessage, error) {
	var m DeliveryMessage
	return &m, json.Unmarshal(data, &m)
}

func UnmarshalEvent(data []byte) (*EventMessage, error) {
	var m EventMessage
	return &m, json.Unmarshal(data, &m)
}
