package hermenats

import "encoding/json"

// SendMessage is published to notification.send by the Admin service.
type SendMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	UserID         string          `json:"user_id"`
	GroupID        string          `json:"group_id"`
	Content        MessageContent  `json:"content"`
	Metadata       MessageMetadata `json:"metadata"`
	Data           map[string]any  `json:"data,omitempty"`
	Channels       []string        `json:"channels,omitempty"`
	Attempt        int             `json:"attempt"`
}

type MessageContent struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	ActionURL   *string `json:"action_url,omitempty"`
	ActionLabel *string `json:"action_label,omitempty"`
}

type MessageMetadata struct {
	Group string `json:"group"`
	Type  string `json:"type,omitempty"`
}

// DeliveryMessage is published to delivery.{channel} by the Dispatch service.
type DeliveryMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	UserID         string          `json:"user_id"`
	Channel        string          `json:"channel"`
	Content        MessageContent  `json:"content"`
	Metadata       MessageMetadata `json:"metadata"`
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
