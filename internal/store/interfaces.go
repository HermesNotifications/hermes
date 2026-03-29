package store

import (
	"context"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
)

// TenantRepository defines operations for managing tenants.
type TenantRepository interface {
	CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error)
	GetTenantByID(ctx context.Context, id string) (*models.Tenant, error)
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
}

// UserRepository defines operations for managing users.
type UserRepository interface {
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
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
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)
	UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error
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
	TenantRepository
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
