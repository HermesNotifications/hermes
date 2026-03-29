package send_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/send"
)

// mockStore implements send.SendStore with in-memory storage.
type mockStore struct {
	apiKeys []models.APIKey
}

func (m *mockStore) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	for _, k := range m.apiKeys {
		if k.ID == id {
			return &k, nil
		}
	}
	return nil, fmt.Errorf("api key not found: %s", id)
}

func newTestServer(t *testing.T) *send.Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	store := &mockStore{}
	// Pass nil for nats, cache, pool — tests use SetSkipAuth and don't need real connections.
	srv := send.NewServer(store, nil, nil, nil, "test-hmac-secret", logger)
	srv.SetSkipAuth(true)
	return srv
}
