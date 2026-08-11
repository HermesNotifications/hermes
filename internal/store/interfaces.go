// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package store

import (
	"context"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
)

// OrganizationRepository defines operations for managing organizations.
type OrganizationRepository interface {
	CreateOrganization(ctx context.Context, id, name string) (*models.Organization, error)
	GetOrganizationByID(ctx context.Context, id string) (*models.Organization, error)
	EnsureOrganization(ctx context.Context, id string) (*models.Organization, error)
	ListOrganizations(ctx context.Context) ([]models.Organization, error)
	CountUsersByOrganization(ctx context.Context) (map[string]int, error)
}

// SubscriptionCategoryRepository defines operations for managing subscription categories.
type SubscriptionCategoryRepository interface {
	CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
	GetCategoryBySlug(ctx context.Context, slug string) (*models.SubscriptionCategory, error)
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	DeleteCategory(ctx context.Context, id string) error
}

// SubscriptionRepository defines operations for managing subscriptions.
type SubscriptionRepository interface {
	CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
	UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
}

// TemplateRepository defines operations for managing notification templates.
type TemplateRepository interface {
	CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error)
	GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error)
	ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error)
	UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error
	GetTemplateContent(ctx context.Context, templateID string) (map[string]map[string]string, error)
	SetTemplateContent(ctx context.Context, templateID string, content map[string]map[string]string) error
}

// UserRepository defines operations for managing users.
type UserRepository interface {
	EnsureUser(ctx context.Context, organizationID, externalID string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
	ListUsers(ctx context.Context, organizationID string) ([]models.User, error)
	GetUserContacts(ctx context.Context, userID string) (map[string]string, error)
	SetUserContact(ctx context.Context, userID, addressKey, address string) error
}

// UserSubscriptionRepository defines operations for managing user subscription preferences.
type UserSubscriptionRepository interface {
	GetUserSubscription(ctx context.Context, userID, subscriptionID string) (*models.UserSubscription, error)
	GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error)
	SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error)
	DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error
}

// NotificationRepository defines operations for the notification write path.
type NotificationRepository interface {
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, organizationID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)
	ListRecentNotifications(ctx context.Context, limit int) ([]models.Notification, error)
	UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error
	UpdateNotificationRouting(ctx context.Context, n *models.Notification) error
	FailNotification(ctx context.Context, notificationID string) error
}

// StatusUpdate represents a notification status change for batch processing.
type StatusUpdate struct {
	NotificationID string
	NewStatus      models.NotificationStatus
	EventTime      time.Time
}

// EventRepository defines operations for notification event storage and status updates.
type EventRepository interface {
	InsertEvent(ctx context.Context, notificationID, channel, event, severity string, metadata []byte) error
	InsertEvents(ctx context.Context, events []models.NotificationEvent) error
	UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error
	BatchUpdateNotificationStatuses(ctx context.Context, updates []StatusUpdate) error
	DeleteEventsOlderThan(ctx context.Context, before time.Time, batchSize int) (int64, error)
}

// InboxRepository defines operations for the user-facing inbox read path.
type InboxRepository interface {
	ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error)
	UnreadCount(ctx context.Context, userID string) (int, error)
	MarkRead(ctx context.Context, userID, notificationID string) (bool, error)
	MarkUnread(ctx context.Context, userID, notificationID string) (bool, error)
	Archive(ctx context.Context, userID, notificationID string) (bool, error)
	Unarchive(ctx context.Context, userID, notificationID string) (bool, error)
	SoftDelete(ctx context.Context, userID, notificationID string) (bool, error)
	MarkAllRead(ctx context.Context, userID string) error
}

// AuthRepository defines operations for API keys and JWT signing keys.
type AuthRepository interface {
	CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error
	ListActiveJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error)
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}

// Repository is the composite interface satisfied by any complete store backend.
type Repository interface {
	OrganizationRepository
	SubscriptionCategoryRepository
	SubscriptionRepository
	TemplateRepository
	UserRepository
	UserSubscriptionRepository
	NotificationRepository
	EventRepository
	InboxRepository
	AuthRepository
}
