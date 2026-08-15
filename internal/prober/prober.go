// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Package prober measures the notification pipeline the way a user experiences it: from
// POST /v1/send returning to the frame arriving on a websocket.
//
// Every latency Hermes emits today is per-hop. Send reports how long it took to publish to
// JetStream (~2ms, whatever happens downstream); the inbox worker reports how long its handler
// ran; Centrifugo reports its own API latency. None of them, alone or summed, answers "did the
// notification arrive, and how long did it take" — the gap between the last hop and the client
// is exactly where a wrong channel, an unsubscribed user or a wedged consumer hides.
//
// That number has existed before, but only inside k6: `ws_push_e2e_latency`, measured during
// load tests. It took four separate defect fixes to make it real, three of which had it
// silently evaluating zero samples while reporting a green threshold. This package is that
// measurement as a continuously-running service, so the pipeline is verified end to end
// between load tests rather than only during them.
package prober

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	id "github.com/hermesnotifications/hermes/internal/id/v2"
	"github.com/hermesnotifications/hermes/internal/observability"
)

// Enough entropy that two probers pointed at the same user cannot collide, which is the case
// during a rolling restart of this Deployment.
var probeIDGen = id.NewGenerator(id.Config{Prefix: "prb", RandBits: 48})

// MetadataKey carries the probe correlator on the notification.
//
// Metadata is the right channel for it because dispatch echoes it to the client verbatim
// (ADR 0019) — it is the only field that survives the whole pipeline and comes back out. The
// load-test harness reached the same conclusion the hard way: it first passed the send
// timestamp through a module-scope map, which cannot work when the sender and the receiver are
// different execution contexts.
//
// Note this prober does NOT put a timestamp in the metadata, only an opaque id. One process
// owns both ends, so it can time the round trip against its own monotonic clock and never has
// to trust a timestamp that crossed a process boundary. That removes clock skew from the
// measurement entirely.
const MetadataKey = "hermes_probe_id"

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/prober")

var (
	// e2eDuration is the number this whole package exists to produce.
	e2eDuration, _ = meter.Float64Histogram(
		"hermes.probe.e2e.duration",
		metric.WithDescription("POST /v1/send returning to the notification frame arriving on the websocket."),
		metric.WithUnit("s"),
	)

	// sendDuration separates "the API is slow" from "the pipeline is slow". Without it a rise
	// in e2e latency is unattributable, and the two have entirely different causes: send is a
	// thin ingestion layer whose latency reflects auth and the JetStream publish, while the
	// rest is dispatch, the worker and Centrifugo.
	sendDuration, _ = meter.Float64Histogram(
		"hermes.probe.send.duration",
		metric.WithDescription("Duration of the POST /v1/send call alone."),
		metric.WithUnit("s"),
	)

	// results is what alerting keys on. A probe that is never delivered produces no latency
	// sample at all, so a histogram alone cannot see the failure this package exists to
	// catch — silence and health look identical. Counting outcomes makes loss explicit.
	results, _ = meter.Int64Counter(
		"hermes.probe.results",
		metric.WithDescription("Probe outcomes by result: received, lost, or send_error."),
		metric.WithUnit("1"),
	)

	// connected is 1 while the probe's websocket is subscribed. A prober that has silently
	// lost its subscription reports 100% loss, which is indistinguishable from a total
	// pipeline outage until you can see this.
	connected, _ = meter.Int64UpDownCounter(
		"hermes.probe.connected",
		metric.WithDescription("1 while the prober holds a subscribed websocket."),
		metric.WithUnit("1"),
	)
)

// Config is what the prober needs to exercise a full round trip.
type Config struct {
	AdminURL      string // mints the user JWT, and auto-creates the org and user on first call
	SendURL       string
	CentrifugoURL string // ws:// or wss:// connection endpoint
	APIKey        string

	OrganizationID string
	UserID         string // external id; the internal one comes back inside the JWT

	// Interval between probes. Each one creates a real notification row, so this is also a
	// write-rate decision: 30s is 2,880 rows/day. Pair it with hermes.cleanup.
	Interval time.Duration
	// Timeout after which an unanswered probe is counted lost rather than waited on forever.
	// Must exceed the worst end-to-end latency you would still call healthy.
	Timeout time.Duration
}

// Prober runs the send→receive loop. One instance owns one websocket subscription.
type Prober struct {
	cfg    Config
	http   *http.Client
	logger *slog.Logger

	mu       sync.Mutex
	inFlight map[string]time.Time // probe id -> send completed at
}

func New(cfg Config, logger *slog.Logger) *Prober {
	return &Prober{
		cfg: cfg,
		// Instrumented so a failing probe produces a trace reaching into the
		// service it probed, rather than a bare latency number. The prober targets
		// fixed internal Services, so the default client metrics stay bounded.
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		logger:   logger,
		inFlight: make(map[string]time.Time),
	}
}

// Run holds a subscription and probes until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) error {
	token, internalUserID, err := p.mintToken(ctx)
	if err != nil {
		return fmt.Errorf("mint token: %w", err)
	}

	client := centrifuge.NewJsonClient(p.cfg.CentrifugoURL, centrifuge.Config{})
	defer client.Close()
	client.SetToken(token)

	// `user#<internal id>` is Centrifugo's user-limited channel convention and the same
	// channel the inbox widget subscribes to — deliberately, so the prober fails when real
	// clients would. Subscribing to a channel of its own would verify a path nobody uses.
	channel := "user#" + internalUserID
	sub, err := client.NewSubscription(channel)
	if err != nil {
		return fmt.Errorf("new subscription: %w", err)
	}
	sub.OnPublication(func(e centrifuge.PublicationEvent) { p.onPublication(ctx, e) })

	sub.OnSubscribed(func(centrifuge.SubscribedEvent) {
		connected.Add(ctx, 1)
		p.logger.Info("probe subscribed", "channel", channel)
	})
	sub.OnUnsubscribed(func(e centrifuge.UnsubscribedEvent) {
		connected.Add(ctx, -1)
		p.logger.Warn("probe unsubscribed", "channel", channel, "reason", e.Reason)
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := sub.Subscribe(); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	// Swept more often than the timeout so a lost probe is reported near when it was lost
	// rather than up to a full timeout later.
	sweep := time.NewTicker(p.cfg.Timeout / 2)
	defer sweep.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.probe(ctx)
		case <-sweep.C:
			p.expire(ctx)
		}
	}
}

// probe sends one notification and records when it went out.
func (p *Prober) probe(ctx context.Context) {
	probeID := probeIDGen.New()

	// Registered BEFORE the send returns, because at a p50 of 5ms the frame can arrive before
	// the HTTP response does. Registering afterwards loses those to the expiry sweep and
	// reports loss on the healthiest possible pipeline.
	started := time.Now()
	p.mu.Lock()
	p.inFlight[probeID] = started
	p.mu.Unlock()

	if err := p.send(ctx, probeID); err != nil {
		p.mu.Lock()
		delete(p.inFlight, probeID)
		p.mu.Unlock()
		results.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "send_error")))
		p.logger.Error("probe send failed", "error", err)
		return
	}
	sendDuration.Record(ctx, time.Since(started).Seconds())
}

func (p *Prober) onPublication(ctx context.Context, e centrifuge.PublicationEvent) {
	var payload struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(e.Data, &payload); err != nil {
		return
	}
	probeID, _ := payload.Metadata[MetadataKey].(string)
	if probeID == "" {
		// Not ours. The probe user should see nothing else, but a shared user or a
		// misconfigured recipient would land here, and silently ignoring it is right.
		return
	}

	p.mu.Lock()
	started, ok := p.inFlight[probeID]
	delete(p.inFlight, probeID)
	p.mu.Unlock()
	if !ok {
		// Already expired, or a duplicate. Delivery is at-least-once, so a redelivered
		// frame arriving after the first is expected and must not be counted twice.
		return
	}

	e2eDuration.Record(ctx, time.Since(started).Seconds())
	results.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "received")))
}

// expire counts probes that never came back.
func (p *Prober) expire(ctx context.Context) {
	deadline := time.Now().Add(-p.cfg.Timeout)

	p.mu.Lock()
	var lost int
	for probeID, started := range p.inFlight {
		if started.Before(deadline) {
			delete(p.inFlight, probeID)
			lost++
		}
	}
	p.mu.Unlock()

	if lost > 0 {
		results.Add(ctx, int64(lost), metric.WithAttributes(attribute.String("result", "lost")))
		p.logger.Warn("probes lost", "count", lost, "timeout", p.cfg.Timeout)
	}
}

func (p *Prober) send(ctx context.Context, probeID string) error {
	body, err := json.Marshal(map[string]any{
		"to": map[string]string{
			"organization_id": p.cfg.OrganizationID,
			"user_id":         p.cfg.UserID,
		},
		"content": map[string]string{
			"title": "Synthetic probe",
			"body":  "Continuous end-to-end pipeline check. Safe to ignore.",
		},
		// Pinned to inbox. The probe measures the realtime path, and an email or SMS channel
		// would route it to a provider that either costs money or, on an evaluation install
		// with no SMTP host, retries forever and looks like a delivery-tier failure.
		"channels": []string{"inbox"},
		"metadata": map[string]any{MetadataKey: probeID},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.SendURL+"/v1/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	// Deliberately no X-Idempotency-Key: each probe is a distinct event, and a key would let
	// the dedup layer answer from cache without the pipeline running at all — which is
	// precisely the thing being measured.

	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// mintToken exchanges the API key for a user JWT and reads the internal user id out of it.
//
// Via the admin API rather than the database on purpose: the endpoint auto-creates the
// organization and the user on first call, so the prober is self-bootstrapping and needs no
// seeding step, and it exercises the same token exchange every real client performs.
func (p *Prober) mintToken(ctx context.Context) (token, internalUserID string, err error) {
	body, err := json.Marshal(map[string]string{
		"organization_id": p.cfg.OrganizationID,
		"user_id":         p.cfg.UserID,
	})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.AdminURL+"/v1/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("auth/token returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if out.Token == "" {
		return "", "", fmt.Errorf("auth/token returned an empty token")
	}

	// The channel is keyed by the INTERNAL user id, which the caller never supplies — it is
	// the JWT's subject. Parsed unverified because this is Hermes' own token, fetched over a
	// trusted call moments ago; verifying it here would mean giving the prober the signing
	// secret for no gain. Subscribing to the external id instead is the exact defect that made
	// the load-test harness measure zero samples while reporting green.
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(out.Token, claims); err != nil {
		return "", "", fmt.Errorf("parse token: %w", err)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", "", fmt.Errorf("token has no subject")
	}
	return out.Token, sub, nil
}
