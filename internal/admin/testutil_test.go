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
	categories    []models.SubscriptionCategory
	subscriptions []models.Subscription
	templates     []models.NotificationTemplate
	users         []models.User
	notifications []models.Notification
	events        []models.NotificationEvent
	apiKeys       []models.APIKey
}

// --- Tenants ---

func (m *mockStore) CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error) {
	t := models.Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	m.tenants = append(m.tenants, t)
	return &t, nil
}

func (m *mockStore) GetTenantByID(ctx context.Context, id string) (*models.Tenant, error) {
	for _, t := range m.tenants {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("tenant not found: %s", id)
}

func (m *mockStore) EnsureTenant(ctx context.Context, id string) (*models.Tenant, error) {
	for _, t := range m.tenants {
		if t.ID == id {
			return &t, nil
		}
	}
	t := models.Tenant{ID: id, Name: id, CreatedAt: time.Now()}
	m.tenants = append(m.tenants, t)
	return &t, nil
}

// --- Subscription Categories ---

func (m *mockStore) CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := models.SubscriptionCategory{
		ID: fmt.Sprintf("sct-%d", len(m.categories)+1), Slug: slug, Name: name,
		DefaultChannels: defaultChannels, DefaultState: defaultState, SortOrder: sortOrder,
		CreatedAt: time.Now(),
	}
	m.categories = append(m.categories, c)
	return &c, nil
}

func (m *mockStore) GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	for _, c := range m.categories {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("category not found: %s", id)
}

func (m *mockStore) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	return m.categories, nil
}

func (m *mockStore) UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	for i, c := range m.categories {
		if c.ID == id {
			m.categories[i].Name = name
			m.categories[i].DefaultChannels = defaultChannels
			m.categories[i].DefaultState = defaultState
			m.categories[i].SortOrder = sortOrder
			updated := m.categories[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("category not found: %s", id)
}

func (m *mockStore) DeleteCategory(ctx context.Context, id string) error {
	for i, c := range m.categories {
		if c.ID == id {
			m.categories = append(m.categories[:i], m.categories[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("category not found: %s", id)
}

// --- Subscriptions ---

func (m *mockStore) CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error) {
	s := models.Subscription{
		ID: fmt.Sprintf("sub-%d", len(m.subscriptions)+1), CategoryID: categoryID,
		Slug: slug, Name: name, SortOrder: sortOrder, CreatedAt: time.Now(),
	}
	m.subscriptions = append(m.subscriptions, s)
	return &s, nil
}

func (m *mockStore) GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error) {
	for _, s := range m.subscriptions {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("subscription not found: %s", id)
}

func (m *mockStore) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	var result []models.Subscription
	for _, s := range m.subscriptions {
		if s.CategoryID == categoryID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error) {
	for i, s := range m.subscriptions {
		if s.ID == id {
			m.subscriptions[i].Name = name
			m.subscriptions[i].SortOrder = sortOrder
			updated := m.subscriptions[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("subscription not found: %s", id)
}

func (m *mockStore) DeleteSubscription(ctx context.Context, id string) error {
	for i, s := range m.subscriptions {
		if s.ID == id {
			m.subscriptions = append(m.subscriptions[:i], m.subscriptions[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("subscription not found: %s", id)
}

// --- Templates ---

func (m *mockStore) CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	t := *input
	t.ID = fmt.Sprintf("ntpl-%d", len(m.templates)+1)
	t.CreatedAt = time.Now()
	m.templates = append(m.templates, t)
	return &t, nil
}

func (m *mockStore) GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error) {
	for _, t := range m.templates {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", id)
}

func (m *mockStore) GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	for _, t := range m.templates {
		if t.Slug == slug {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", slug)
}

func (m *mockStore) ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error) {
	return m.templates, nil
}

func (m *mockStore) UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	for i, t := range m.templates {
		if t.ID == input.ID {
			m.templates[i] = *input
			updated := m.templates[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", input.ID)
}

func (m *mockStore) DeleteTemplate(ctx context.Context, id string) error {
	for i, t := range m.templates {
		if t.ID == id {
			m.templates = append(m.templates[:i], m.templates[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("template not found: %s", id)
}

// --- Tenants (additional) ---

func (m *mockStore) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	return m.tenants, nil
}

func (m *mockStore) CountUsersByTenant(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int)
	for _, u := range m.users {
		counts[u.TenantID]++
	}
	return counts, nil
}

// --- Users ---

func (m *mockStore) ListUsers(ctx context.Context, tenantID string) ([]models.User, error) {
	if tenantID == "" {
		return m.users, nil
	}
	var result []models.User
	for _, u := range m.users {
		if u.TenantID == tenantID {
			result = append(result, u)
		}
	}
	return result, nil
}

func (m *mockStore) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	for _, u := range m.users {
		if u.ID == userID {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user not found: %s", userID)
}

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

func (m *mockStore) GetNotificationByID(ctx context.Context, id string) (*models.Notification, error) {
	for _, n := range m.notifications {
		if n.ID == id {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("notification not found: %s", id)
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

func (m *mockStore) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error) {
	k := models.APIKey{
		ID:          id,
		KeyHash:     keyHash,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   time.Now(),
	}
	m.apiKeys = append(m.apiKeys, k)
	return &k, nil
}

func (m *mockStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	return m.apiKeys, nil
}

func (m *mockStore) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	for _, k := range m.apiKeys {
		if k.ID == id {
			return &k, nil
		}
	}
	return nil, fmt.Errorf("api key not found: %s", id)
}

func (m *mockStore) DeleteAPIKey(ctx context.Context, id string) error {
	for i, k := range m.apiKeys {
		if k.ID == id {
			m.apiKeys = append(m.apiKeys[:i], m.apiKeys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("api key not found: %s", id)
}

// --- JWT Signing Keys ---

func (m *mockStore) EnsureHermesSigningKey(ctx context.Context, secret string) error {
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
	// Pass nil for cache, pool — most handlers don't need them.
	// Pass store as tenants (mockStore implements EnsureTenant).
	srv := admin.NewServer(store, store, nil, nil, []byte("test-jwt-secret"), "test-hmac-secret", logger)
	srv.SetSkipAuth(true)
	return srv
}

func newTestServerWithStore(t *testing.T, store *mockStore) *admin.Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := admin.NewServer(store, store, nil, nil, []byte("test-jwt-secret"), "test-hmac-secret", logger)
	srv.SetSkipAuth(true)
	return srv
}
