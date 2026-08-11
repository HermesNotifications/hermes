// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/hermes-notifications/hermes/internal/observability"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	conn *nats.Conn
	js   jetstream.JetStream

	// streamMaxBytes bounds each work stream. Zero means DefaultStreamMaxBytes.
	streamMaxBytes int64

	// done is closed by Close to unblock the worker pools and fetcher callbacks
	// started by Subscribe so they exit instead of leaking. closeOnce guards it.
	done      chan struct{}
	closeOnce sync.Once
}

// DefaultStreamMaxBytes bounds each of the three work streams on disk.
//
// The NATS PVC is 5Gi (deploy/k8s/base/infra/nats.yaml). Three work streams at
// 512 MiB plus the 1 GiB DLQ is 2.5 GiB, half the volume, which leaves room for
// the store's own overhead and for a stream to sit at its ceiling without
// crowding the others. At roughly 1 KiB per message that is ~500k queued
// messages per stream — orders of magnitude beyond a healthy backlog, so it
// binds only when something downstream is genuinely broken.
const DefaultStreamMaxBytes int64 = 512 << 20

type StreamConfig struct {
	Name     string
	Subjects []string
}

var Streams = []StreamConfig{
	{Name: "NOTIFICATIONS", Subjects: []string{"notification.send"}},
	{Name: "DELIVERY", Subjects: []string{"delivery.email", "delivery.sms", "delivery.inbox"}},
	{Name: "EVENTS", Subjects: []string{"notification.events"}},
}

// Option configures a Connect call. Options exist so the transport security added in
// ADR 0005 phase 2 is supplied by the caller from configuration rather than hardcoded
// here, and so the plaintext development path stays reachable by passing nothing.
type Option func(*connectOptions)

type connectOptions struct {
	nats           []nats.Option
	streamMaxBytes int64
	// errs collects option failures that cannot be expressed as a nats.Option — loading
	// an NKey seed happens eagerly, unlike a CA bundle, which nats.go reads at dial
	// time. Collected rather than returned so Connect reports every problem at once.
	errs []error
}

// WithCABundle trusts the PEM bundle at path when verifying the NATS server's
// certificate, and requires TLS for the connection.
//
// An empty path is a no-op, which is what keeps `make infra-up` working: local NATS has
// no certificate and HERMES_NATS_CA_BUNDLE is unset. The private CA that cert-manager
// uses for nats.hermes.svc is in no system trust store, so in a real deployment this is
// how the connection can be verified at all — but a missing bundle fails the connection
// loudly rather than downgrading it, so an unset variable cannot put data on the wire.
func WithCABundle(path string) Option {
	return func(o *connectOptions) {
		if path == "" {
			return
		}
		o.nats = append(o.nats, nats.RootCAs(path))
	}
}

// WithStreamMaxBytes overrides the per-work-stream disk ceiling. Zero or less
// keeps DefaultStreamMaxBytes. Only the provisioner's value has any effect,
// since it is the one identity permitted to create or update a stream.
func WithStreamMaxBytes(n int64) Option {
	return func(o *connectOptions) {
		if n > 0 {
			o.streamMaxBytes = n
		}
	}
}

func Connect(url string, opts ...Option) (*Client, error) {
	var co connectOptions
	for _, opt := range opts {
		opt(&co)
	}

	if err := errors.Join(co.errs...); err != nil {
		return nil, err
	}

	nc, err := nats.Connect(url, co.nats...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &Client{conn: nc, js: js, streamMaxBytes: co.streamMaxBytes, done: make(chan struct{})}, nil
}

func (c *Client) SetupStreams(ctx context.Context) error {
	maxBytes := c.streamMaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultStreamMaxBytes
	}

	for _, s := range Streams {
		_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:      s.Name,
			Subjects:  s.Subjects,
			Retention: jetstream.WorkQueuePolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    7 * 24 * time.Hour,
			MaxBytes:  maxBytes,
			// DiscardNew, not DiscardOld. These streams carry accepted work: a
			// notification the API already returned 202 for. Discarding the oldest
			// message to make room would silently destroy that work, and the
			// caller would have been told it was accepted. Rejecting the newest
			// instead pushes the failure back to the publisher, which can still
			// tell its caller — see the 503 in internal/send/handler_send.go.
			//
			// The DLQ takes the opposite choice for the opposite reason: nothing
			// is waiting on a dead letter, so there keeping the oldest evidence
			// beats keeping the newest.
			Discard: jetstream.DiscardNew,
		})
		if err != nil {
			return fmt.Errorf("create stream %s: %w", s.Name, err)
		}
	}
	// The DLQ stream is deliberately NOT in Streams: nothing Subscribes to it
	// in-process (operators consume it manually via the nats CLI), and keeping
	// it out of the subject→stream lookup prevents accidental consumers.
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      dlqStreamName,
		Subjects:  []string{"dlq.>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
		MaxBytes:  1 << 30, // 1 GiB; oldest dead letters discarded first
		Discard:   jetstream.DiscardOld,
	})
	if err != nil {
		return fmt.Errorf("create stream %s: %w", dlqStreamName, err)
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, subject string, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data}
	_, span := observability.InjectNATS(ctx, msg)
	defer span.End()

	_, err := c.js.PublishMsg(ctx, msg)
	observability.RecordError(span, err)
	return err
}

// PermanentError is an interface for errors that should not be retried.
// When a handler returns an error implementing this interface with Permanent() == true,
// the message is terminated (will never be redelivered).
type PermanentError interface {
	Permanent() bool
}

// maxDeliveries is the maximum number of times a message will be delivered
// before being dead-lettered. A var so tests can lower it (see
// export_test.go); production code never mutates it.
var maxDeliveries = 10

// retryDelay returns an exponential backoff delay with jitter.
// Base delay doubles each attempt (1s, 2s, 4s, …) capped at 240s,
// then jitter picks a uniform random duration in [base/2, base].
func retryDelay(attempt uint64) time.Duration {
	base := time.Second << (attempt - 1)
	if base > 240*time.Second {
		base = 240 * time.Second
	}
	half := base / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// DeliveryInfo is passed to handlers so they can react to delivery context.
type DeliveryInfo struct {
	// Attempt is the 1-based delivery attempt number.
	Attempt uint64
	// LastAttempt is true when this is the final delivery before the message is dead-lettered.
	LastAttempt bool
}

// defaultPrefetch is the fetcher's in-flight buffer when a SubscribeConfig leaves
// Prefetch unset. Large enough that the 50%-threshold refill keeps the pull
// pipeline full (no per-message round trip), small enough to avoid hoarding.
const defaultPrefetch = 64

// SubscribeConfig configures a Subscribe consumer. A single fetcher loop pulls
// from the durable consumer and hands each message to a pool of Workers; the
// fetcher blocks when every worker is busy, so in-flight work is bounded by the
// pool and by MaxAckPending. Prefetch decouples fetching from processing so the
// pull pipeline stays full without one consumer hoarding the whole backlog.
type SubscribeConfig struct {
	Subject  string
	Consumer string
	// MaxAckPending caps server-side unacked messages. Raised if below
	// Prefetch+Workers so the server never throttles below the in-flight budget.
	MaxAckPending int
	// Workers is the size of the processing pool. < 1 is treated as 1.
	Workers int
	// Prefetch is the fetcher's in-flight buffer (PullMaxMessages). < 1 uses
	// defaultPrefetch. Batching consumers (event-writer) set this high.
	Prefetch int
}

func streamForSubject(subject string) string {
	for _, s := range Streams {
		for _, subj := range s.Subjects {
			if subj == subject {
				return s.Name
			}
		}
	}
	return ""
}

func (c *Client) Subscribe(cfg SubscribeConfig, handler func(ctx context.Context, data []byte, info DeliveryInfo) error) error {
	streamName := streamForSubject(cfg.Subject)
	if streamName == "" {
		return fmt.Errorf("no stream found for subject %s", cfg.Subject)
	}

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	prefetch := cfg.Prefetch
	if prefetch < 1 {
		prefetch = defaultPrefetch
	}
	// The server must allow at least as many unacked messages as we can hold in
	// flight (prefetch buffer + one being processed per worker), or it would
	// throttle delivery below our own backpressure point.
	maxAckPending := cfg.MaxAckPending
	if minPending := prefetch + workers; maxAckPending < minPending {
		maxAckPending = minPending
	}

	cons, err := c.js.CreateOrUpdateConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       cfg.Consumer,
		FilterSubject: cfg.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: maxAckPending,
		MaxDeliver:    maxDeliveries,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	// One fetcher (the Consume loop) hands messages to a bounded worker pool over
	// an unbuffered channel. When all workers are busy the hand-off blocks, which
	// stops the fetcher draining its prefetch buffer — natural backpressure. The
	// prefetch buffer (PullMaxMessages) lives inside Consume and keeps the pull
	// pipeline full so the fetcher never stalls on a per-message round trip.
	work := make(chan jetstream.Msg)
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case msg := <-work:
					c.processMessage(streamName, cfg.Consumer, cfg.Subject, msg, handler)
				case <-c.done:
					return
				}
			}
		}()
	}

	_, err = cons.Consume(func(msg jetstream.Msg) {
		select {
		case work <- msg:
		case <-c.done:
			// Shutting down: drop without ack; the message is redelivered later.
		}
	}, jetstream.PullMaxMessages(prefetch))
	if err != nil {
		return fmt.Errorf("start consumer: %w", err)
	}
	return nil
}

// processMessage runs one message through the handler and applies the ack policy.
// Success acks; a terminal error is dead-lettered then terminated; any other
// error (including a recovered handler panic) is nak'd for retry with backoff.
func (c *Client) processMessage(streamName, consumer, subject string, msg jetstream.Msg, handler func(ctx context.Context, data []byte, info DeliveryInfo) error) {
	ctx, span := observability.ExtractNATS(context.Background(), msg.Headers(), msg.Subject())
	defer span.End()

	meta, _ := msg.Metadata()
	attempt := uint64(1)
	if meta != nil {
		attempt = meta.NumDelivered
	}
	info := DeliveryInfo{
		Attempt:     attempt,
		LastAttempt: attempt >= uint64(maxDeliveries),
	}

	if err := safeHandle(ctx, handler, msg.Data(), info); err != nil {
		if dead, reason := classify(err, attempt); dead {
			if dlqErr := c.publishDeadLetter(ctx, streamName, consumer, subject, reason, attempt, err, msg.Data()); dlqErr != nil {
				// Never destroy a message we failed to preserve. Past MaxDeliver
				// the Nak is a no-op and the message lingers in the source stream
				// until MaxAge — same as before this feature existed. Note: if a
				// PermanentError fails to publish here at an early attempt, the
				// redelivery may eventually dead-letter it as max_deliveries
				// rather than terminated. The safety invariant still holds, and
				// HermesDLQPublishFailure will already be firing.
				_ = msg.NakWithDelay(retryDelay(attempt))
				return
			}
			_ = msg.Term()
			return
		}
		_ = msg.NakWithDelay(retryDelay(attempt))
		return
	}
	_ = msg.Ack()
}

// safeHandle invokes handler, converting a panic into a (retryable) error so one
// poison message can't take down a whole worker — it gets nak'd and, after
// MaxDeliver attempts, dead-lettered like any other persistent failure.
func safeHandle(ctx context.Context, handler func(ctx context.Context, data []byte, info DeliveryInfo) error, data []byte, info DeliveryInfo) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return handler(ctx, data, info)
}

func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.conn.Close()
}
