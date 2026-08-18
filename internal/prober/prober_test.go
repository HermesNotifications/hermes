// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package prober

import (
	"context"
	"encoding/json"
	"fmt"
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

// The bug behind #173: the connection token was minted once and set statically, but it lives
// 4h ± 10%. When it expired the SDK had nothing to present, raised
// ConfigurationError{"GetToken must be set to handle expired token"} on every reconnect, and
// the prober reported 100% loss for 41 hours while looking healthy from outside.
//
// Asserting the callback exists is most of the value; asserting it actually re-mints is the
// rest, because a callback that returns the same expired string would fail identically.
func TestClientConfig_RefreshesTheTokenRatherThanReusingIt(t *testing.T) {
	var mints int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mints++
		// Distinct per mint via the JWT id, not by decorating the signed string: mintToken
		// parses what it gets back, so an appended suffix is not a different token, it is a
		// malformed one.
		claims := jwt.RegisteredClaims{
			Subject:   "usr_internal123",
			ID:        fmt.Sprintf("mint-%d", mints),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(4 * time.Hour)),
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + signed + `"}`))
	}))
	defer srv.Close()

	p := testProber(t, Config{AdminURL: srv.URL, APIKey: "k", OrganizationID: "org1", UserID: "u"})
	cfg := p.clientConfig(context.Background(), "initial-token")

	if cfg.Token != "initial-token" {
		t.Errorf("Token = %q, want the token Run already minted", cfg.Token)
	}
	if cfg.GetToken == nil {
		t.Fatal("GetToken must be set, or an expired token permanently wedges the prober")
	}

	first, err := cfg.GetToken(centrifuge.ConnectionTokenEvent{})
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	second, err := cfg.GetToken(centrifuge.ConnectionTokenEvent{})
	if err != nil {
		t.Fatalf("GetToken (second): %v", err)
	}

	if mints != 2 {
		t.Errorf("admin was called %d times, want one mint per refresh", mints)
	}
	if first == second {
		t.Error("each refresh must fetch a new token, not replay the previous one")
	}
	if first == "initial-token" {
		t.Error("refresh returned the original token instead of a fresh one")
	}
}

// A refresh that fails must surface as an error so the SDK backs off and retries. Returning a
// stale token instead would recreate the original bug with extra steps.
func TestClientConfig_RefreshFailurePropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := testProber(t, Config{AdminURL: srv.URL, APIKey: "k"})
	cfg := p.clientConfig(context.Background(), "initial-token")

	// Guarded so a regression that drops the callback fails with this message rather than a
	// nil-dereference panic several frames away.
	if cfg.GetToken == nil {
		t.Fatal("GetToken must be set, or an expired token permanently wedges the prober")
	}
	got, err := cfg.GetToken(centrifuge.ConnectionTokenEvent{})
	if err == nil {
		t.Fatal("a failed refresh must return an error so the SDK retries")
	}
	if got != "" {
		t.Errorf("returned token %q alongside an error; must be empty", got)
	}
}

// A dead subscription reports every probe lost, forever, and until #173 that arrived as an
// unbroken drip of identical WARN lines -- which is exactly what a genuinely-down pipeline
// looks like, and what nobody reads. Crossing the threshold has to be distinguishable, and it
// has to happen once rather than on every sweep.
func TestExpire_EscalatesOnceWhenLossBecomesContinuous(t *testing.T) {
	p := testProber(t, Config{Timeout: time.Millisecond})
	ctx := context.Background()

	expireN := func(n int) {
		for i := 0; i < n; i++ {
			p.inFlight[fmt.Sprintf("prb_%d", i)] = time.Now().Add(-time.Second)
		}
		p.expire(ctx)
	}

	expireN(lostStreakThreshold - 1)
	if p.streakEscalated {
		t.Fatalf("escalated at %d losses, below the threshold of %d", p.lostStreak, lostStreakThreshold)
	}

	expireN(1)
	if !p.streakEscalated {
		t.Fatalf("should have escalated at %d losses (threshold %d)", p.lostStreak, lostStreakThreshold)
	}

	// Still escalated, but the flag must not re-arm: the condition is continuous, the news is
	// not, and a log line per sweep is the noise this replaced.
	before := p.lostStreak
	expireN(1)
	if p.lostStreak != before+1 {
		t.Errorf("streak = %d, want %d", p.lostStreak, before+1)
	}
	if !p.streakEscalated {
		t.Error("escalation flag must stay set while loss continues")
	}
}

// One delivery proves the subscription is alive again, so the next outage escalates afresh
// rather than being swallowed by a flag left over from the last one.
func TestOnPublication_ResetsTheLossStreak(t *testing.T) {
	p := testProber(t, Config{Timeout: time.Millisecond})
	ctx := context.Background()

	p.lostStreak = lostStreakThreshold + 5
	p.streakEscalated = true

	p.inFlight["prb_live"] = time.Now()
	data, err := json.Marshal(map[string]any{"metadata": map[string]any{MetadataKey: "prb_live"}})
	if err != nil {
		t.Fatal(err)
	}
	p.onPublication(ctx, centrifuge.PublicationEvent{Publication: centrifuge.Publication{Data: data}})

	if p.lostStreak != 0 || p.streakEscalated {
		t.Fatalf("a received probe must reset the streak, got streak=%d escalated=%v", p.lostStreak, p.streakEscalated)
	}
}

// A publication that matches nothing in flight -- a redelivery, or a probe already expired --
// is not evidence the subscription recovered, so it must not clear the streak.
func TestOnPublication_UnmatchedFrameDoesNotResetTheStreak(t *testing.T) {
	p := testProber(t, Config{Timeout: time.Millisecond})

	p.lostStreak = lostStreakThreshold + 1
	p.streakEscalated = true

	data, err := json.Marshal(map[string]any{"metadata": map[string]any{MetadataKey: "prb_not_in_flight"}})
	if err != nil {
		t.Fatal(err)
	}
	p.onPublication(context.Background(), centrifuge.PublicationEvent{Publication: centrifuge.Publication{Data: data}})

	if p.lostStreak == 0 || !p.streakEscalated {
		t.Fatal("an unmatched frame must not count as recovery")
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
