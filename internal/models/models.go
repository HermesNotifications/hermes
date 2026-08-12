// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package models

import (
	"encoding/json"
	"time"
)

type Organization struct {
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

	// RateLimitPerSecond and RateLimitBurst override the service default for
	// this credential. Nil means "use the default" — the column is nullable so
	// that unset stays distinguishable from a deliberately chosen value.
	RateLimitPerSecond *int `json:"rate_limit_per_second,omitempty"`
	RateLimitBurst     *int `json:"rate_limit_burst,omitempty"`
}

// RateLimitOverride is a credential's own rate limit.
//
// Both fields are optional and independent: a key may raise its burst while keeping the
// default sustained rate. A nil field means "use the service default", which is the same
// sentinel middleware.ResolveLimit applies to a zero override, so an unset limit needs no
// special case anywhere downstream.
//
// It exists as a struct rather than two more positional arguments because the credential
// is where per-namespace and per-plan limits will attach when ADR 0012's namespace phase
// lands; adding a field here will not break every call site again.
type RateLimitOverride struct {
	PerSecond *int
	Burst     *int
}

type User struct {
	ID             string            `json:"id"`
	OrganizationID string            `json:"organization_id"`
	ExternalID     string            `json:"external_id"`
	Contacts       map[string]string `json:"contacts,omitempty"`
	Locale         *string           `json:"locale,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
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
	ID              string                       `json:"id"`
	SubscriptionID  *string                      `json:"subscription_id,omitempty"`
	Slug            string                       `json:"slug"`
	Name            string                       `json:"name"`
	DefaultChannels []string                     `json:"default_channels"`
	Content         map[string]map[string]string `json:"content,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
}

type Notification struct {
	ID             string             `json:"id"`
	OrganizationID string             `json:"organization_id"`
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
	// Metadata is sender-supplied and opaque; see NotificationMetadata for the two keys
	// Hermes reads.
	Metadata    NotificationMetadata `json:"metadata,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	SentAt      *time.Time           `json:"sent_at,omitempty"`
	DeliveredAt *time.Time           `json:"delivered_at,omitempty"`
	ReadAt      *time.Time           `json:"read_at,omitempty"`
	ArchivedAt  *time.Time           `json:"archived_at,omitempty"`
	DeletedAt   *time.Time           `json:"deleted_at,omitempty"`
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
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Algorithm           string    `json:"algorithm"`
	Secret              string    `json:"-"`
	UserIDClaim         string    `json:"user_id_claim"`
	OrganizationIDClaim string    `json:"organization_id_claim"`
	Active              bool      `json:"active"`
	CreatedAt           time.Time `json:"created_at"`
}
