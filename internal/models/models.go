// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package models

import (
	"encoding/json"
	"time"
)

type Tenant struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DefaultLocale string    `json:"default_locale,omitempty"`
	Settings      []byte    `json:"settings,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type APIKey struct {
	ID          string    `json:"id"`
	KeyHash     string    `json:"-"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type User struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	ExternalID string  `json:"external_id"`
	Email      *string `json:"email,omitempty"`
	Phone      *string `json:"phone,omitempty"`
	// Contacts is the normalized contact-point map: address key ("email",
	// "phone") -> address. Added in phase 2a alongside Email/Phone, which are
	// removed in phase 2e.
	Contacts  map[string]string `json:"contacts,omitempty"`
	Locale    *string           `json:"locale,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type SubscriptionCategory struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	DefaultState    string    `json:"default_state"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
}

type Subscription struct {
	ID         string    `json:"id"`
	CategoryID string    `json:"category_id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	SortOrder  int       `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
}

type NotificationTemplate struct {
	ID              string   `json:"id"`
	SubscriptionID  *string  `json:"subscription_id,omitempty"`
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
	EmailSubject    *string  `json:"email_subject,omitempty"`
	EmailBody       *string  `json:"email_body,omitempty"`
	SMSBody         *string  `json:"sms_body,omitempty"`
	InboxTitle      *string  `json:"inbox_title,omitempty"`
	InboxBody       *string  `json:"inbox_body,omitempty"`
	// Content is the normalized per-channel content: channel slug -> field key
	// -> template string. Added in phase 2a alongside the fixed Email*/SMS*/
	// Inbox* fields, which are removed in phase 2e.
	Content   map[string]map[string]string `json:"content,omitempty"`
	CreatedAt time.Time                    `json:"created_at"`
}

type Notification struct {
	ID             string             `json:"id"`
	TenantID       string             `json:"tenant_id"`
	UserID         string             `json:"user_id"`
	TemplateID     *string            `json:"template_id,omitempty"`
	CategoryID     string             `json:"category_id"`
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
	ID             string          `json:"id"`
	NotificationID string          `json:"notification_id"`
	Channel        string          `json:"channel"`
	Event          string          `json:"event"`
	Severity       string          `json:"severity"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type UserSubscription struct {
	UserID         string    `json:"user_id"`
	SubscriptionID string    `json:"subscription_id"`
	OptedIn        bool      `json:"opted_in"`
	CreatedAt      time.Time `json:"created_at"`
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
