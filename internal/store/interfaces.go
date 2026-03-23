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

// GroupRepository defines operations for managing notification groups.
type GroupRepository interface {
	CreateGroup(ctx context.Context, slug, name string, defaultChannels []string) (*models.NotificationGroup, error)
	GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error)
	GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error)
	ListGroups(ctx context.Context) ([]models.NotificationGroup, error)
	UpdateGroup(ctx context.Context, id, name string, defaultChannels []string) (*models.NotificationGroup, error)
}

// TypeRepository defines operations for managing notification types (templates).
type TypeRepository interface {
	CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error)
	GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error)
	ListTypes(ctx context.Context) ([]models.NotificationType, error)
	UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	DeleteType(ctx context.Context, id string) error
}

// UserRepository defines operations for managing users.
type UserRepository interface {
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
}

// PreferenceRepository defines operations for managing user notification preferences.
type PreferenceRepository interface {
	GetUserPreference(ctx context.Context, userID, groupID string) (*models.UserPreference, error)
	GetUserPreferences(ctx context.Context, userID string) ([]models.UserPreference, error)
	SetUserPreference(ctx context.Context, userID, groupID string, channels []string) (*models.UserPreference, error)
	DeleteUserPreference(ctx context.Context, userID, groupID string) error
}

// NotificationRepository defines operations for the notification write path.
type NotificationRepository interface {
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)
	UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error
}

// EventRepository defines operations for notification event storage and status updates.
type EventRepository interface {
	InsertEvent(ctx context.Context, notificationID, channel, event, severity string, metadata []byte) error
	InsertEvents(ctx context.Context, events []models.NotificationEvent) error
	UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error
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
	CreateAPIKey(ctx context.Context, keyHash, name string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	ListActiveJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error)
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}

// Repository is the composite interface satisfied by any complete store backend.
type Repository interface {
	TenantRepository
	GroupRepository
	TypeRepository
	UserRepository
	PreferenceRepository
	NotificationRepository
	EventRepository
	InboxRepository
	AuthRepository
}
