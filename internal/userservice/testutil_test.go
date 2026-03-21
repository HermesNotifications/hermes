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
	users       []models.User
	preferences []models.UserPreference
	groups      []models.NotificationGroup
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

func (m *mockUserStore) GetUserPreferences(ctx context.Context, userID string) ([]models.UserPreference, error) {
	var result []models.UserPreference
	for _, p := range m.preferences {
		if p.UserID == userID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockUserStore) SetUserPreference(ctx context.Context, userID, groupID string, channels []string) (*models.UserPreference, error) {
	for i, p := range m.preferences {
		if p.UserID == userID && p.GroupID == groupID {
			m.preferences[i].Channels = channels
			updated := m.preferences[i]
			return &updated, nil
		}
	}
	pref := models.UserPreference{
		UserID:   userID,
		GroupID:  groupID,
		Channels: channels,
	}
	m.preferences = append(m.preferences, pref)
	return &pref, nil
}

func (m *mockUserStore) DeleteUserPreference(ctx context.Context, userID, groupID string) error {
	for i, p := range m.preferences {
		if p.UserID == userID && p.GroupID == groupID {
			m.preferences = append(m.preferences[:i], m.preferences[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("delete user preference: %w", pgx.ErrNoRows)
}

func (m *mockUserStore) ListGroups(ctx context.Context) ([]models.NotificationGroup, error) {
	return m.groups, nil
}

const testUserID = "test-user-id"

func newTestServer(t *testing.T) (*userservice.Server, *mockUserStore) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	email := "user@example.com"
	store := &mockUserStore{
		users: []models.User{
			{
				ID:         testUserID,
				TenantID:   "tenant-1",
				ExternalID: "ext-1",
				Email:      &email,
				CreatedAt:  time.Now(),
			},
		},
		groups: []models.NotificationGroup{
			{ID: "group-1", Slug: "alerts", Name: "Alerts", DefaultChannels: []string{"email"}},
		},
		preferences: []models.UserPreference{
			{UserID: testUserID, GroupID: "group-1", Channels: []string{"email", "inbox"}},
		},
	}
	srv := userservice.NewServer(store, nil, nil, logger)
	srv.SetSkipAuth(true)
	return srv, store
}
