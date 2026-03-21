package models

import "time"

type Tenant struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DefaultLocale string    `json:"default_locale,omitempty"`
	Settings      []byte    `json:"settings,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"-"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ExternalID string    `json:"external_id"`
	Email      *string   `json:"email,omitempty"`
	Phone      *string   `json:"phone,omitempty"`
	Locale     *string   `json:"locale,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type NotificationGroup struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	CreatedAt       time.Time `json:"created_at"`
}

type NotificationType struct {
	ID           string    `json:"id"`
	GroupID      string    `json:"group_id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	EmailSubject *string   `json:"email_subject,omitempty"`
	EmailBody    *string   `json:"email_body,omitempty"`
	SMSBody      *string   `json:"sms_body,omitempty"`
	InboxTitle   *string   `json:"inbox_title,omitempty"`
	InboxBody    *string   `json:"inbox_body,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Notification struct {
	ID             string             `json:"id"`
	TenantID       string             `json:"tenant_id"`
	UserID         string             `json:"user_id"`
	TypeID         *string            `json:"type_id,omitempty"`
	GroupID        string             `json:"group_id"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	ActionURL      *string            `json:"action_url,omitempty"`
	ActionLabel    *string            `json:"action_label,omitempty"`
	IdempotencyKey *string            `json:"idempotency_key,omitempty"`
	Channels       []string           `json:"channels"`
	Status         NotificationStatus `json:"status"`
	CreatedAt      time.Time          `json:"created_at"`
	SentAt         *time.Time         `json:"sent_at,omitempty"`
	DeliveredAt    *time.Time         `json:"delivered_at,omitempty"`
	ReadAt         *time.Time         `json:"read_at,omitempty"`
	ArchivedAt     *time.Time         `json:"archived_at,omitempty"`
	DeletedAt      *time.Time         `json:"deleted_at,omitempty"`
}

type NotificationEvent struct {
	ID             string    `json:"id"`
	NotificationID string    `json:"notification_id"`
	Channel        string    `json:"channel"`
	Event          string    `json:"event"`
	Severity       string    `json:"severity"`
	Metadata       []byte    `json:"metadata,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type UserPreference struct {
	UserID   string   `json:"user_id"`
	GroupID  string   `json:"group_id"`
	Channels []string `json:"channels"`
}

type JWTSigningKey struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Algorithm     string    `json:"algorithm"`
	Secret        string    `json:"-"`
	UserIDClaim   string    `json:"user_id_claim"`
	TenantIDClaim string    `json:"tenant_id_claim"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
}
