// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package prober

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/golang-jwt/jwt/v5"
)

func testProber(t *testing.T, cfg Config) *Prober {
	t.Helper()
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Second
	}
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// The correlation path is what the k6 harness got wrong four separate times, three of which
// left it evaluating zero samples while reporting a green threshold. A probe that quietly
// matches nothing looks exactly like a healthy pipeline, so these assert the matching itself
// rather than the metric values.
func TestOnPublication_MatchesProbeAndClearsIt(t *testing.T) {
	p := testProber(t, Config{})
	ctx := context.Background()

	p.inFlight["prb_abc"] = time.Now().Add(-10 * time.Millisecond)

	data, err := json.Marshal(map[string]any{
		"type":     "notification.new",
		"metadata": map[string]any{MetadataKey: "prb_abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.onPublication(ctx, centrifuge.PublicationEvent{Publication: centrifuge.Publication{Data: data}})

	if len(p.inFlight) != 0 {
		t.Fatalf("matched probe should be cleared, still have %d", len(p.inFlight))
	}
}

// Delivery is at-least-once, so the same frame can arrive twice. The second must not be
// counted, and must not panic on the missing entry.
func TestOnPublication_RedeliveryIsIgnored(t *testing.T) {
	p := testProber(t, Config{})
	ctx := context.Background()
	p.inFlight["prb_abc"] = time.Now()

	data, _ := json.Marshal(map[string]any{"metadata": map[string]any{MetadataKey: "prb_abc"}})
	ev := centrifuge.PublicationEvent{Publication: centrifuge.Publication{Data: data}}

	p.onPublication(ctx, ev)
	p.onPublication(ctx, ev) // must be a no-op

	if len(p.inFlight) != 0 {
		t.Fatalf("expected empty inFlight, got %d", len(p.inFlight))
	}
}

// A notification that is not a probe must leave in-flight probes untouched. Getting this wrong
// would clear a real probe on unrelated traffic and under-report loss.
func TestOnPublication_IgnoresForeignNotifications(t *testing.T) {
	p := testProber(t, Config{})
	ctx := context.Background()
	p.inFlight["prb_abc"] = time.Now()

	for _, body := range []string{
		`{"type":"notification.new","id":"n1"}`,
		`{"type":"notification.new","metadata":{"level":"info"}}`,
		`{"type":"inbox.updated","unread_count":3}`,
		`not json at all`,
	} {
		p.onPublication(ctx, centrifuge.PublicationEvent{Publication: centrifuge.Publication{Data: []byte(body)}})
	}

	if len(p.inFlight) != 1 {
		t.Fatalf("foreign notifications must not clear probes; inFlight=%d", len(p.inFlight))
	}
}

func TestExpire_OnlyRemovesProbesPastTheDeadline(t *testing.T) {
	p := testProber(t, Config{Timeout: 100 * time.Millisecond})

	p.inFlight["old"] = time.Now().Add(-time.Second)
	p.inFlight["fresh"] = time.Now()

	p.expire(context.Background())

	if _, ok := p.inFlight["old"]; ok {
		t.Error("expired probe should have been removed")
	}
	if _, ok := p.inFlight["fresh"]; !ok {
		t.Error("in-window probe must be kept")
	}
}

// The channel is keyed by the JWT subject (the INTERNAL user id), not by the external id the
// caller supplies. Subscribing to the external one is the defect that made every load-test
// send push to a channel nobody was subscribed to.
func TestMintToken_UsesJWTSubjectNotConfiguredUserID(t *testing.T) {
	const internalID = "usr_internal123"

	claims := jwt.RegisteredClaims{
		Subject:   internalID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + signed + `","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	p := testProber(t, Config{
		AdminURL:       srv.URL,
		APIKey:         "test-key",
		OrganizationID: "org1",
		UserID:         "external-user",
	})

	token, userID, err := p.mintToken(context.Background())
	if err != nil {
		t.Fatalf("mintToken: %v", err)
	}
	if userID != internalID {
		t.Errorf("channel must key on the JWT subject: got %q, want %q", userID, internalID)
	}
	if token != signed {
		t.Error("returned token should be the one the server issued")
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("API key must be sent as a bearer token, got %q", gotAuth)
	}
	if !json.Valid([]byte(gotBody)) {
		t.Errorf("request body should be JSON, got %q", gotBody)
	}
}

func TestMintToken_ErrorsOnNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	p := testProber(t, Config{AdminURL: srv.URL, APIKey: "k"})
	if _, _, err := p.mintToken(context.Background()); err == nil {
		t.Fatal("expected an error on a 403")
	}
}

// The send must pin channels to inbox and carry the correlator, or the probe measures a path
// no realtime client uses -- or, on an install with no SMTP host, routes itself into the email
// worker's infinite retry loop.
func TestSend_PinsInboxChannelAndCarriesProbeID(t *testing.T) {
	var payload struct {
		To       map[string]string `json:"to"`
		Channels []string          `json:"channels"`
		Metadata map[string]any    `json:"metadata"`
	}
	var idempotency string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idempotency = r.Header.Get("X-Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"notification_id":"n1"}`))
	}))
	defer srv.Close()

	p := testProber(t, Config{SendURL: srv.URL, APIKey: "k", OrganizationID: "org1", UserID: "u1"})
	if err := p.send(context.Background(), "prb_xyz"); err != nil {
		t.Fatalf("send: %v", err)
	}

	if len(payload.Channels) != 1 || payload.Channels[0] != "inbox" {
		t.Errorf("channels must be pinned to inbox, got %v", payload.Channels)
	}
	if payload.Metadata[MetadataKey] != "prb_xyz" {
		t.Errorf("probe id must travel in metadata, got %v", payload.Metadata)
	}
	if payload.To["organization_id"] != "org1" || payload.To["user_id"] != "u1" {
		t.Errorf("recipient not passed through: %v", payload.To)
	}
	// An idempotency key would let the dedup layer answer from cache without the pipeline
	// running, which is exactly what the probe is measuring.
	if idempotency != "" {
		t.Errorf("probe must not send an idempotency key, got %q", idempotency)
	}
}

func TestSend_ErrorsOnNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := testProber(t, Config{SendURL: srv.URL, APIKey: "k"})
	if err := p.send(context.Background(), "prb_1"); err == nil {
		t.Fatal("expected an error on a 429")
	}
}
