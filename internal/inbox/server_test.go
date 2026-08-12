// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermesnotifications/hermes/internal/inbox"
)

// ADR 0005 phase 3. Under per-service NATS credentials, every connection costs an NKey user
// and a set of subject permissions — so a service that holds a bus client it never uses is
// asking for a credential granted for nothing. The inbox service was doing exactly that:
// cmd/inbox connected to NATS and handed the client to NewServer, which stored it in a field
// nothing read.
//
// This test pins the constructor's shape so the client cannot come back by accident. It is a
// compile-time assertion as much as a runtime one: NewServer takes no *messaging.Client, and
// the server still serves.
func TestNewServer_TakesNoMessagingClient(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := inbox.NewServer(&mockInboxStore{}, nil, nil, nil, logger)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	// A request the router answers without a store, so this stays about wiring: an
	// unauthenticated call must be rejected by the JWT middleware rather than panic on a
	// missing dependency.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/inbox", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d", rec.Code)
	}
}
