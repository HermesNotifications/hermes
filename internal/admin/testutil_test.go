package admin_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/models"
)

// mockStore implements admin.AdminStore with in-memory storage.
type mockStore struct {
	tenants       []models.Tenant
	groups        []models.NotificationGroup
	types         []models.NotificationType
	users         []models.User
	notifications []models.Notification
	events        []models.NotificationEvent
	apiKeys       []models.APIKey
	jwtKeys       []models.JWTSigningKey
}

// --- Tenants ---

func (m *mockStore) GetTenantByID(ctx context.Context, id string) (*models.Tenant, error) {
	for _, t := range m.tenants {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("tenant not found: %s", id)
}

// --- Groups ---

func (m *mockStore) CreateGroup(ctx context.Context, slug, name string, channels []string) (*models.NotificationGroup, error) {
	g := models.NotificationGroup{
		ID:              fmt.Sprintf("grp-%d", len(m.groups)+1),
		Slug:            slug,
		Name:            name,
		DefaultChannels: channels,
		CreatedAt:       time.Now(),
	}
	m.groups = append(m.groups, g)
	return &g, nil
}

func (m *mockStore) GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error) {
	for _, g := range m.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("group not found: %s", id)
}

func (m *mockStore) GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error) {
	for _, g := range m.groups {
		if g.Slug == slug {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("group not found: %s", slug)
}

func (m *mockStore) ListGroups(ctx context.Context) ([]models.NotificationGroup, error) {
	return m.groups, nil
}

func (m *mockStore) UpdateGroup(ctx context.Context, id, name string, channels []string) (*models.NotificationGroup, error) {
	for i, g := range m.groups {
		if g.ID == id {
			m.groups[i].Name = name
			m.groups[i].DefaultChannels = channels
			updated := m.groups[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("group not found: %s", id)
}

// --- Types ---

func (m *mockStore) CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	t := *input
	t.ID = fmt.Sprintf("typ-%d", len(m.types)+1)
	t.CreatedAt = time.Now()
	m.types = append(m.types, t)
	return &t, nil
}

func (m *mockStore) GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error) {
	for _, t := range m.types {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("type not found: %s", id)
}

func (m *mockStore) GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error) {
	for _, t := range m.types {
		if t.Slug == slug {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("type not found: %s", slug)
}

func (m *mockStore) ListTypes(ctx context.Context) ([]models.NotificationType, error) {
	return m.types, nil
}

func (m *mockStore) UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	for i, t := range m.types {
		if t.ID == input.ID {
			m.types[i] = *input
			updated := m.types[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("type not found: %s", input.ID)
}

func (m *mockStore) DeleteType(ctx context.Context, id string) error {
	for i, t := range m.types {
		if t.ID == id {
			m.types = append(m.types[:i], m.types[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("type not found: %s", id)
}

// --- Users ---

func (m *mockStore) EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error) {
	for _, u := range m.users {
		if u.TenantID == tenantID && u.ExternalID == externalID {
			return &u, nil
		}
	}
	u := models.User{
		ID:         fmt.Sprintf("usr-%d", len(m.users)+1),
		TenantID:   tenantID,
		ExternalID: externalID,
		CreatedAt:  time.Now(),
	}
	m.users = append(m.users, u)
	return &u, nil
}

// --- Notifications ---

func (m *mockStore) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	created := *n
	if created.ID == "" {
		created.ID = fmt.Sprintf("ntf-%d", len(m.notifications)+1)
	}
	created.CreatedAt = time.Now()
	m.notifications = append(m.notifications, created)
	return &created, nil
}

func (m *mockStore) GetNotificationByID(ctx context.Context, id string) (*models.Notification, error) {
	for _, n := range m.notifications {
		if n.ID == id {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("notification not found: %s", id)
}

func (m *mockStore) GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error) {
	for _, n := range m.notifications {
		if n.TenantID == tenantID && n.IdempotencyKey != nil && *n.IdempotencyKey == key {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("notification not found for key: %s", key)
}

func (m *mockStore) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	var out []models.NotificationEvent
	for _, e := range m.events {
		if e.NotificationID == notificationID {
			out = append(out, e)
		}
	}
	return out, nil
}

// --- API Keys ---

func (m *mockStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	return m.apiKeys, nil
}

// --- JWT Signing Keys ---

func (m *mockStore) CreateJWTSigningKey(ctx context.Context, name, algorithm, secret, userIDClaim, tenantIDClaim string) (*models.JWTSigningKey, error) {
	k := models.JWTSigningKey{
		ID:            fmt.Sprintf("jwtk-%d", len(m.jwtKeys)+1),
		Name:          name,
		Algorithm:     algorithm,
		Secret:        secret,
		UserIDClaim:   userIDClaim,
		TenantIDClaim: tenantIDClaim,
		Active:        true,
		CreatedAt:     time.Now(),
	}
	m.jwtKeys = append(m.jwtKeys, k)
	return &k, nil
}

func (m *mockStore) ListJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error) {
	return m.jwtKeys, nil
}

func (m *mockStore) DeleteJWTSigningKey(ctx context.Context, id string) error {
	for i, k := range m.jwtKeys {
		if k.ID == id {
			m.jwtKeys = append(m.jwtKeys[:i], m.jwtKeys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("jwt signing key not found: %s", id)
}

func (m *mockStore) EnsureHermesSigningKey(ctx context.Context, secret string) error {
	for i, k := range m.jwtKeys {
		if k.ID == "hermes-internal" {
			m.jwtKeys[i].Secret = secret
			return nil
		}
	}
	m.jwtKeys = append(m.jwtKeys, models.JWTSigningKey{
		ID:            "hermes-internal",
		Name:          "hermes-internal",
		Algorithm:     "HS256",
		Secret:        secret,
		UserIDClaim:   "sub",
		TenantIDClaim: "tenant_id",
		Active:        true,
		CreatedAt:     time.Now(),
	})
	return nil
}

// --- Test helpers ---

func newTestServer(t *testing.T) *admin.Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	store := &mockStore{
		tenants: []models.Tenant{
			{ID: "test-tenant-id", Name: "Test Tenant", CreatedAt: time.Now()},
		},
	}
	// Pass nil for nats, cache, pool — most handlers don't need them.
	srv := admin.NewServer(store, nil, nil, nil, []byte("test-jwt-secret"), logger)
	srv.SetSkipAuth(true)
	return srv
}
