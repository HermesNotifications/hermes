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

	"github.com/hermesnotifications/hermes/internal/observability"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

	// mu guards consumeCtxs, which Subscribe appends to and Drain reads.
	mu          sync.Mutex
	consumeCtxs []jetstream.ConsumeContext
	// inflight counts messages handed to a worker pool but not yet finished, so Drain can
	// wait for them instead of the process exiting mid-handler.
	inflight sync.WaitGroup
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

// natsDrainTimeout bounds the connection-level flush at the end of Drain. Distinct from, and
// deliberately shorter than, the caller's overall drain budget: by the time it runs, handlers
// have already finished and only buffered acks and publishes remain.
const natsDrainTimeout = 10 * time.Second

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

	// Bounds conn.Drain() in Drain(), which otherwise waits indefinitely for the flush.
	co.nats = append(co.nats, nats.DrainTimeout(natsDrainTimeout))

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

// StreamOptions are the deployment-shaped properties of the streams: how many peers hold each
// one, and how much disk it may occupy. Both are environment decisions rather than code
// decisions, so they arrive from configuration instead of being hardcoded here.
type StreamOptions struct {
	// Replicas is the JetStream replication factor. 0 or 1 means a single peer.
	//
	// Deliberately explicit rather than derived from the observed cluster size: a provisioner
	// Job that happened to run during a NATS rolling restart could see one server and silently
	// downgrade every stream to R=1.
	Replicas int
	// MaxBytes caps each work stream's on-disk size. 0 uses DefaultStreamMaxBytes.
	MaxBytes int64
	// AllowReplicaChange permits altering the replication factor of a stream that already
	// exists. Off by default: see the comment in SetupStreams.
	AllowReplicaChange bool
}

// SetupStreams creates or updates the pipeline's streams.
func (c *Client) SetupStreams(ctx context.Context, opts StreamOptions) error {
	replicas := opts.Replicas
	if replicas < 1 {
		replicas = 1
	}
	// Either API may supply the ceiling, and both are in use: this call's StreamOptions, or
	// the WithStreamMaxBytes connect option that cmd/natsprovision passes. The explicit
	// per-call value wins; the connect option is the deployment-wide default behind it.
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = c.streamMaxBytes
	}
	if maxBytes <= 0 {
		maxBytes = DefaultStreamMaxBytes
	}

	for _, s := range Streams {
		cfg := jetstream.StreamConfig{
			Name:      s.Name,
			Subjects:  s.Subjects,
			Retention: jetstream.WorkQueuePolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    7 * 24 * time.Hour,
			Replicas:  replicas,
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
		}
		if err := c.upsertStream(ctx, cfg, opts.AllowReplicaChange); err != nil {
			return err
		}
	}

	// The DLQ stream is deliberately NOT in Streams: nothing Subscribes to it
	// in-process (operators consume it manually via the nats CLI), and keeping
	// it out of the subject→stream lookup prevents accidental consumers.
	//
	// DiscardOld here, unlike the work streams: the DLQ is a forensic record, and one that has
	// filled up must keep accepting new dead letters rather than reject them — a rejected dead
	// letter is a message destroyed with no trace, which is the one outcome the DLQ exists to
	// prevent.
	dlq := jetstream.StreamConfig{
		Name:      dlqStreamName,
		Subjects:  []string{"dlq.>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
		MaxBytes:  1 << 30, // 1 GiB; oldest dead letters discarded first
		Discard:   jetstream.DiscardOld,
		Replicas:  replicas,
	}
	return c.upsertStream(ctx, dlq, opts.AllowReplicaChange)
}

// upsertStream creates the stream, or updates it — but refuses to change an existing stream's
// replication factor unless explicitly allowed.
//
// Changing Replicas on a file-backed stream holding days of messages is a peer catch-up that
// moves real bytes between servers. That is a maintenance operation someone should choose and
// watch, not something a routine deploy starts unattended. The reverse matters more: without
// this guard, pointing a single-node staging config at production would quietly strip every
// stream back to one replica and nothing would report it.
func (c *Client) upsertStream(ctx context.Context, cfg jetstream.StreamConfig, allowReplicaChange bool) error {
	existing, err := c.js.Stream(ctx, cfg.Name)
	switch {
	case errors.Is(err, jetstream.ErrStreamNotFound):
		if _, err := c.js.CreateStream(ctx, cfg); err != nil {
			return fmt.Errorf("create stream %s: %w", cfg.Name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect stream %s: %w", cfg.Name, err)
	}

	if current := existing.CachedInfo().Config.Replicas; current != cfg.Replicas && !allowReplicaChange {
		return fmt.Errorf(
			"stream %s has %d replica(s) but the configuration asks for %d: changing the "+
				"replication factor migrates the whole stream between peers, so it is a "+
				"maintenance operation rather than a deploy. Run it deliberately with "+
				"HERMES_NATS_STREAM_REPLICAS_ALLOW_CHANGE=true, or with `nats stream update`",
			cfg.Name, current, cfg.Replicas)
	}

	if _, err := c.js.UpdateStream(ctx, cfg); err != nil {
		return fmt.Errorf("update stream %s: %w", cfg.Name, err)
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
	// HandlerTimeout bounds a single handler invocation. < 1 uses defaultHandlerTimeout.
	//
	// Without it a handler derives from context.Background() and a wedged provider call --
	// an SMTP server that accepts the connection and then says nothing -- occupies a pool
	// worker forever, so the consumer quietly loses capacity one stuck message at a time.
	HandlerTimeout time.Duration
	// AckWait is how long JetStream waits for an ack before redelivering. < 1 derives it
	// from HandlerTimeout.
	//
	// It must exceed HandlerTimeout or the server redelivers a message that is still being
	// processed, and the handler's side effect -- an email, a webhook, a Centrifugo publish
	// -- happens twice. The JetStream default of 30s is shorter than several of Hermes'
	// providers can take, which is why leaving this unset was a live duplicate source.
	AckWait time.Duration
}

const (
	// defaultHandlerTimeout is generous enough for an SMTP conversation or a customer's
	// webhook endpoint, and short enough that a hung one frees its worker the same minute.
	defaultHandlerTimeout = 30 * time.Second
	// ackWaitMultiplier derives AckWait from HandlerTimeout when a caller does not set it.
	// Double leaves room for the handler to run to its own deadline and still ack.
	ackWaitMultiplier = 2
)

// resolveTimeouts fills in the defaults and enforces the one invariant that matters:
// AckWait must outlast HandlerTimeout, or every slow message is processed twice.
func (cfg SubscribeConfig) resolveTimeouts() (handlerTimeout, ackWait time.Duration) {
	handlerTimeout = cfg.HandlerTimeout
	if handlerTimeout < 1 {
		handlerTimeout = defaultHandlerTimeout
	}
	ackWait = cfg.AckWait
	if ackWait < 1 {
		ackWait = handlerTimeout * ackWaitMultiplier
	}
	if ackWait <= handlerTimeout {
		ackWait = handlerTimeout * ackWaitMultiplier
	}
	return handlerTimeout, ackWait
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

	handlerTimeout, ackWait := cfg.resolveTimeouts()

	cons, err := c.js.CreateOrUpdateConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       cfg.Consumer,
		FilterSubject: cfg.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: maxAckPending,
		MaxDeliver:    maxDeliveries,
		AckWait:       ackWait,
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
					c.processMessage(streamName, cfg.Consumer, cfg.Subject, msg, handler, handlerTimeout)
					c.inflight.Done()
				case <-c.done:
					return
				}
			}
		}()
	}

	consumeCtx, err := cons.Consume(func(msg jetstream.Msg) {
		// Counted here, in the fetcher, rather than in the worker that picks the message up.
		// A worker cannot increment until it has been scheduled, which leaves a window where a
		// message has left the fetcher but is not yet counted -- and Drain, waiting on the
		// counter, would sail straight past it. Stop() guarantees this callback is no longer
		// invoked, so counting here means the total can only fall once draining begins.
		c.inflight.Add(1)
		select {
		case work <- msg:
		case <-c.done:
			// Shutting down: drop without ack; the message is redelivered later.
			c.inflight.Done()
		}
	}, jetstream.PullMaxMessages(prefetch))
	if err != nil {
		return fmt.Errorf("start consumer: %w", err)
	}

	// Retained so Drain can stop the fetcher. Discarding this was why nothing could stop
	// consuming short of closing the whole connection: the pool kept pulling *new* work for
	// the entire shutdown window.
	c.mu.Lock()
	c.consumeCtxs = append(c.consumeCtxs, consumeCtx)
	c.mu.Unlock()
	return nil
}

// processMessage runs one message through the handler and applies the ack policy.
// Success acks; a terminal error is dead-lettered then terminated; any other
// error (including a recovered handler panic) is nak'd for retry with backoff.
func (c *Client) processMessage(streamName, consumer, subject string, msg jetstream.Msg, handler func(ctx context.Context, data []byte, info DeliveryInfo) error, handlerTimeout time.Duration) {
	ctx, span := observability.ExtractNATS(context.Background(), msg.Headers(), msg.Subject())
	defer span.End()

	// Bounded so one unresponsive provider cannot hold a pool worker indefinitely, and so the
	// handler is guaranteed to return before AckWait expires and JetStream redelivers underneath
	// it. A handler that respects its context turns a hang into an ordinary retryable error.
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()

	meta, _ := msg.Metadata()
	attempt := uint64(1)
	if meta != nil {
		attempt = meta.NumDelivered
	}
	info := DeliveryInfo{
		Attempt:     attempt,
		LastAttempt: attempt >= uint64(maxDeliveries),
	}

	consumerAttrs := metric.WithAttributes(
		attribute.String("stream", streamName),
		attribute.String("consumer", consumer),
	)
	if attempt > 1 {
		redeliveries.Add(ctx, 1, consumerAttrs)
	}
	inflightGauge.Add(ctx, 1, consumerAttrs)
	started := time.Now()
	result := "ok"
	defer func() {
		inflightGauge.Add(ctx, -1, consumerAttrs)
		handlerDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(
			attribute.String("stream", streamName),
			attribute.String("consumer", consumer),
			attribute.String("result", result),
		))
	}()

	if err := safeHandle(ctx, handler, msg.Data(), info); err != nil {
		result = "retry"
		if dead, reason := classify(err, attempt); dead {
			result = "dead_letter"
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

// ErrDrainTimeout reports that handlers were still running when Drain's budget ran out. The
// messages they held are not lost -- they were never acked, so JetStream redelivers them after
// AckWait -- but their side effects will be repeated, which is worth alerting on.
var ErrDrainTimeout = errors.New("messaging: drain timed out with handlers still in flight")

// Drain stops consuming, waits for in-flight handlers, then flushes the connection.
//
// This is the graceful counterpart to Close, and the ordering is the whole point:
//
//  1. Stop every fetcher, so no *new* message is pulled. Previously nothing did this -- the
//     ConsumeContext was discarded -- so a shutting-down pod kept accepting work it had no
//     intention of finishing.
//  2. Close done, releasing any hand-off blocked on a busy pool. Those messages are dropped
//     unacked and redelivered.
//  3. Wait for handlers already running. Without this the process exits mid-handler, and every
//     rolling restart re-executes whatever side effects were in progress -- a sent email, a
//     published Centrifugo event -- when JetStream redelivers.
//  4. Flush pending publishes, so acks and dead letters produced in step 3 actually leave.
//
// Call it before the HTTP server drains, not after: this releases work, that releases traffic.
func (c *Client) Drain(timeout time.Duration) error {
	c.mu.Lock()
	ctxs := make([]jetstream.ConsumeContext, len(c.consumeCtxs))
	copy(ctxs, c.consumeCtxs)
	c.mu.Unlock()

	for _, cc := range ctxs {
		cc.Stop()
	}
	c.closeOnce.Do(func() { close(c.done) })

	if !waitTimeout(&c.inflight, timeout) {
		c.conn.Close()
		return ErrDrainTimeout
	}
	if err := c.conn.Drain(); err != nil {
		return fmt.Errorf("nats drain: %w", err)
	}
	return nil
}

// waitTimeout reports whether wg reached zero before timeout elapsed.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// Healthy reports whether the client currently holds a usable connection to the bus.
//
// A local status read, not a round trip: nats.go maintains this from the connection's own
// state, so a readiness probe calling it every few seconds adds no traffic to a bus that may
// already be the thing in trouble.
func (c *Client) Healthy() error {
	if status := c.conn.Status(); status != nats.CONNECTED {
		return fmt.Errorf("nats connection is %s", status)
	}
	return nil
}

// Close tears the connection down immediately, abandoning in-flight handlers. Prefer Drain on
// any path where the process is shutting down on purpose.
func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.conn.Close()
}
