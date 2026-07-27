// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package hermenats

import (
	"encoding/json"
	"time"
)

// SendMessage is published to notification.send by the Send service.
type SendMessage struct {
	NotificationID string            `json:"notification_id"`
	OrganizationID string            `json:"organization_id"`
	ExternalUserID string            `json:"external_user_id"`
	Contacts       map[string]string `json:"contacts,omitempty"` // per-send address overrides: address key -> address
	Content        *MessageContent   `json:"content,omitempty"`
	Metadata       MessageMetadata   `json:"metadata"`
	Data           map[string]any    `json:"data,omitempty"`
	Channels       []string          `json:"channels,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Attempt        int               `json:"attempt"`
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

// Recipient holds the resolved contact addresses for a notification target,
// keyed by address key ("email", "phone", ...). Marshals to the same JSON
// object shape as the previous fixed struct.
type Recipient map[string]string

// DeliveryMessage is published to delivery.{channel} by the Dispatch service.
type DeliveryMessage struct {
	NotificationID string          `json:"notification_id"`
	OrganizationID string          `json:"organization_id"`
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

// Dead-letter reasons recorded on DeadLetter.Reason.
const (
	// DeadLetterReasonMaxDeliveries marks a message that failed every delivery attempt.
	DeadLetterReasonMaxDeliveries = "max_deliveries"
	// DeadLetterReasonTerminated marks a message rejected with a PermanentError.
	DeadLetterReasonTerminated = "terminated"
)

// DeadLetter wraps a message that exhausted its delivery attempts or was
// permanently rejected by a consumer. Published to "dlq.<original subject>"
// on the DLQ stream by internal/messaging.
type DeadLetter struct {
	Subject  string          `json:"subject"`   // original subject, e.g. "delivery.email"
	Stream   string          `json:"stream"`    // source stream, e.g. "DELIVERY"
	Consumer string          `json:"consumer"`  // durable consumer name
	Reason   string          `json:"reason"`    // DeadLetterReasonMaxDeliveries or DeadLetterReasonTerminated
	Attempts uint64          `json:"attempts"`  // delivery attempts consumed
	Error    string          `json:"error"`     // handler error from the final attempt
	FailedAt time.Time       `json:"failed_at"` // when the message was dead-lettered
	Payload  json.RawMessage `json:"payload"`   // original message body, verbatim
}

func (m *DeadLetter) Marshal() ([]byte, error) { return json.Marshal(m) }

func UnmarshalDeadLetter(data []byte) (*DeadLetter, error) {
	var m DeadLetter
	return &m, json.Unmarshal(data, &m)
}
