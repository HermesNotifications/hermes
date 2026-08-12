// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package send_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/send"
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

// mockPublisher implements send.Publisher for testing.
type mockPublisher struct {
	published []publishedMsg
	err       error
}

type publishedMsg struct {
	Subject string
	Data    []byte
}

func (m *mockPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, publishedMsg{Subject: subject, Data: data})
	return nil
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

func newTestServerWithPublisher(t *testing.T, pub *mockPublisher) *send.Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	store := &mockStore{}
	srv := send.NewServer(store, pub, nil, nil, "test-hmac-secret", logger)
	srv.SetSkipAuth(true)
	return srv
}
