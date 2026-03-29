package userservice_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/userservice"
	"github.com/jackc/pgx/v5"
)

// mockUserStore implements userservice.UserStore with in-memory storage.
type mockUserStore struct {
	users             []models.User
	userSubscriptions []models.UserSubscription
	categories        []models.SubscriptionCategory
	subscriptions     []models.Subscription
}

func (m *mockUserStore) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	for _, u := range m.users {
		if u.ID == userID {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user not found: %s", userID)
}

func (m *mockUserStore) UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error) {
	for i, u := range m.users {
		if u.ID == userID {
			if email != nil {
				m.users[i].Email = email
			}
			if phone != nil {
				m.users[i].Phone = phone
			}
			updated := m.users[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("user not found: %s", userID)
}

func (m *mockUserStore) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	var result []models.UserSubscription
	for _, us := range m.userSubscriptions {
		if us.UserID == userID {
			result = append(result, us)
		}
	}
	return result, nil
}

func (m *mockUserStore) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	for i, us := range m.userSubscriptions {
		if us.UserID == userID && us.SubscriptionID == subscriptionID {
			m.userSubscriptions[i].OptedIn = optedIn
			updated := m.userSubscriptions[i]
			return &updated, nil
		}
	}
	us := models.UserSubscription{
		UserID: userID, SubscriptionID: subscriptionID, OptedIn: optedIn,
	}
	m.userSubscriptions = append(m.userSubscriptions, us)
	return &us, nil
}

func (m *mockUserStore) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	for i, us := range m.userSubscriptions {
		if us.UserID == userID && us.SubscriptionID == subscriptionID {
			m.userSubscriptions = append(m.userSubscriptions[:i], m.userSubscriptions[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("delete user subscription: %w", pgx.ErrNoRows)
}

func (m *mockUserStore) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	return m.categories, nil
}

func (m *mockUserStore) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	var result []models.Subscription
	for _, s := range m.subscriptions {
		if s.CategoryID == categoryID {
			result = append(result, s)
		}
	}
	return result, nil
}

const testUserID = "test-user-id"

func newTestServer(t *testing.T) (*userservice.Server, *mockUserStore) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	email := "user@example.com"
	store := &mockUserStore{
		users: []models.User{
			{ID: testUserID, TenantID: "tenant-1", ExternalID: "ext-1", Email: &email, CreatedAt: time.Now()},
		},
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "general", Name: "General", DefaultChannels: []string{"email", "inbox"}, DefaultState: "on"},
			{ID: "sct-2", Slug: "marketing", Name: "Marketing", DefaultChannels: []string{"email"}, DefaultState: "off"},
		},
		subscriptions: []models.Subscription{
			{ID: "sub-1", CategoryID: "sct-1", Slug: "general", Name: "General"},
			{ID: "sub-2", CategoryID: "sct-2", Slug: "marketing", Name: "Marketing"},
		},
		userSubscriptions: []models.UserSubscription{
			{UserID: testUserID, SubscriptionID: "sub-2", OptedIn: true},
		},
	}
	srv := userservice.NewServer(store, nil, logger)
	srv.SetSkipAuth(true)
	return srv, store
}
