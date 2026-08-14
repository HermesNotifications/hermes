// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Stall detection exists because of an incident this package could not have reported: dispatch
// sat at 1m of CPU with 133,472 messages pending on its consumer, its last log line nearly three
// hours old, while the pod reported Ready with zero restarts. A rollout restart fixed it
// instantly. Every signal Kubernetes had was green, because every signal Kubernetes had —
// /healthz returning a constant 200, /readyz checking that Postgres and the NATS connection were
// reachable — asks whether the process is *up*, and the process was up. Nothing asked whether the
// consumer was still turning.
//
// The trigger was never identified and the wedged pod's logs are gone, so nothing here attempts
// to fix a cause. This detects the shape: work waiting, and none of it being taken.
//
// The distinction that makes the signal safe is between *idle* and *stalled*. An empty queue
// produces no progress and must stay healthy, or every quiet period would restart the fleet.
// So a consumer is unhealthy only when both halves hold, continuously, for the whole window:
//
//	work is waiting for it   AND   it has finished nothing
//
// "Finished" means a handler returned and the message was acked, nak'd or terminated — not that
// the work succeeded. A consumer failing every message and nacking it is retrying, which is a
// different problem with different alerts; the loop is still turning and a restart would not help.
//
// "Waiting" is NumPending + NumAckPending, which costs one CONSUMER.INFO request per poll. Both
// terms are needed. NumPending alone misses the case where every worker is wedged inside a
// handler and the stream behind them is empty: those messages have been delivered, so they are
// ack-pending, and a NumPending-only check would call that consumer idle forever — the exact
// failure this was written for, one degree luckier.

// DefaultConsumerStallTimeout is how long a consumer may hold work while finishing none of it
// before it is reported stalled.
//
// The floor is set by the longest legitimate quiet period a *working* consumer can have. The
// worst case is a consumer whose messages are all in exponential backoff: retryDelay caps at 240s,
// and while those messages sit in backoff they are ack-pending, so "work is waiting" is true and
// nothing completes. Ten minutes clears that 4-minute ceiling by 2.5x, and clears it again on top
// of the kubelet's own failureThreshold. Detecting a wedge in ten minutes instead of three hours
// is the win here; shaving it to five would buy little and spend the entire safety margin.
const DefaultConsumerStallTimeout = 10 * time.Minute

const (
	// maxConsumerPollInterval bounds how often a consumer is inspected. One request per consumer
	// per 30s is nothing next to the message traffic, and the resolution it buys is finer than the
	// ten-minute window needs.
	maxConsumerPollInterval = 30 * time.Second
	// minConsumerPollInterval keeps a deliberately tiny timeout (tests do this) from spinning.
	minConsumerPollInterval = 10 * time.Millisecond
	// consumerPollTimeout bounds one CONSUMER.INFO round trip. A poll that times out is a poll
	// that could not answer, which is treated as "no evidence" rather than as evidence of a stall.
	consumerPollTimeout = 5 * time.Second
)

// pollIntervalFor samples about ten times per window, so a stall is noticed promptly after the
// window elapses rather than up to a full poll later.
func pollIntervalFor(timeout time.Duration) time.Duration {
	interval := timeout / 10
	if interval > maxConsumerPollInterval {
		interval = maxConsumerPollInterval
	}
	if interval < minConsumerPollInterval {
		interval = minConsumerPollInterval
	}
	return interval
}

// errNotConnected is the "no evidence" case that matters most: with the connection down there is
// no way to ask how much work is waiting, and a NATS outage must not be able to restart every
// consumer pod in the fleet at once.
var errNotConnected = errors.New("nats connection is not established")

// consumerProgress is one consumer's answer to "is this thing still turning".
//
// It holds a single timestamp — the last moment there was *evidence of health* — and every input
// either moves that timestamp forward or leaves it alone. Evidence is any of:
//
//   - a message finished (the loop is demonstrably turning),
//   - a poll found no work waiting (nothing to take, so taking nothing is correct),
//   - a poll could not answer (no evidence either way, and the safe reading of "I don't know" is
//     "healthy" — see errNotConnected).
//
// Only one input leaves the timestamp alone: a poll that found work waiting. So the age of that
// timestamp is exactly "how long has this consumer had work it is not finishing", which is the
// quantity the probe compares against timeout.
type consumerProgress struct {
	stream   string
	consumer string
	// cons is the handle the poll asks for NumPending. Nil in the state-machine tests, which
	// drive observe directly rather than through a server.
	cons jetstream.Consumer
	// timeout of 0 disables detection entirely; stall always reports healthy.
	timeout time.Duration
	// now is injected so the state machine can be tested against a clock rather than a sleep.
	now func() time.Time

	mu sync.Mutex
	// lastEvidence is the timestamp described above. Seeded at construction so a consumer that
	// starts into a large backlog gets a full window to make its first ack — this is the whole of
	// the startup grace period, and it is why no restart can happen before timeout of uptime.
	lastEvidence time.Time
	waiting      uint64
	// unknown records that the most recent poll could not answer, so the state is reported
	// honestly rather than as health.
	unknown bool
	// reported and pollReported debounce logging to the transitions rather than every poll, and
	// are separate so a paused detector and a stalled consumer do not mask each other.
	reported     bool
	pollReported bool
	// stopped is set when the client is draining. A shutting-down consumer stops taking work by
	// design, and must not spend its last seconds reporting itself stalled for doing so.
	stopped bool
}

func newConsumerProgress(stream, consumer string, cons jetstream.Consumer, timeout time.Duration) *consumerProgress {
	if timeout < 0 {
		timeout = 0
	}
	p := &consumerProgress{
		stream:   stream,
		consumer: consumer,
		cons:     cons,
		timeout:  timeout,
		now:      time.Now,
	}
	p.lastEvidence = p.now()
	return p
}

// finished records that a handler ran to completion and the message was acked, nak'd or
// terminated. Called from processMessage, which is the one place that is true.
func (p *consumerProgress) finished() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastEvidence = p.now()
	p.unknown = false
}

// observe records one poll. waiting is NumPending + NumAckPending; err non-nil means the poll
// could not answer.
func (p *consumerProgress) observe(waiting uint64, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		// Reset rather than merely refuse to advance. A blip mid-stall discards the evidence
		// gathered so far and starts the window over, which is deliberate: this check's worst
		// outcome is restarting healthy pods, and NATS being unreachable is the moment when every
		// consumer in the fleet would reach that conclusion simultaneously.
		p.unknown = true
		p.lastEvidence = p.now()
		return
	}
	p.unknown = false
	p.waiting = waiting
	if waiting == 0 {
		p.lastEvidence = p.now()
	}
}

// stop retires the monitor: from here on it reports healthy whatever the clock says.
func (p *consumerProgress) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// snapshot returns how long this consumer has gone without evidence of health, and how much work
// it was last seen holding.
func (p *consumerProgress) snapshot() (age time.Duration, waiting uint64, unknown, stopped bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.now().Sub(p.lastEvidence), p.waiting, p.unknown, p.stopped
}

// stall returns a descriptive error when this consumer has held work without finishing any of it
// for longer than timeout, and nil otherwise.
func (p *consumerProgress) stall() error {
	if p.timeout == 0 {
		return nil
	}
	age, waiting, _, stopped := p.snapshot()
	if stopped || age <= p.timeout {
		return nil
	}
	return fmt.Errorf("consumer %s on %s has taken nothing from a backlog of %d for %s",
		p.consumer, p.stream, waiting, age.Round(time.Second))
}

// markReported flips the log debounce and reports whether the caller should log the transition.
func (p *consumerProgress) markReported(stalled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reported == stalled {
		return false
	}
	p.reported = stalled
	return true
}

// watchConsumer polls one consumer until the client shuts down.
//
// It is the only thing in this package that talks to the server without a message to justify it,
// which is why the poll is cheap (CONSUMER.INFO, no payload) and why a failed poll is worth
// nothing rather than worth a restart.
func (c *Client) watchConsumer(p *consumerProgress) {
	ticker := time.NewTicker(pollIntervalFor(p.timeout))
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			p.stop()
			return
		case <-ticker.C:
			c.pollConsumer(p)
		}
	}
}

func (c *Client) pollConsumer(p *consumerProgress) {
	attrs := metric.WithAttributes(
		attribute.String("stream", p.stream),
		attribute.String("consumer", p.consumer),
	)

	switch {
	case c.conn.Status() != nats.CONNECTED:
		// Checked before asking, so a disconnected client spends no time waiting out the poll
		// timeout on a request that cannot leave.
		p.observe(0, errNotConnected)
	default:
		ctx, cancel := context.WithTimeout(context.Background(), consumerPollTimeout)
		info, err := p.cons.Info(ctx)
		cancel()
		if err != nil {
			// Logged at the transition, not every poll: a consumer whose INFO permission is
			// missing would otherwise fill the log with the same line every 30s — and it would be
			// the only warning that stall detection is silently switched off, so it must be
			// visible rather than drowned.
			if p.markPollReported(true) {
				c.logger.Warn("cannot inspect consumer; stall detection paused",
					"stream", p.stream, "consumer", p.consumer, "error", err)
			}
			p.observe(0, err)
			break
		}
		p.markPollReported(false)
		p.observe(info.NumPending+uint64(info.NumAckPending), nil)
	}

	// Recorded on every poll including the ones that could not answer, where observe has just
	// reset the age. The metric and the probe must not be able to disagree: a gauge left stale at
	// its last high value would keep HermesConsumerStalled firing through a NATS outage that
	// liveness is deliberately ignoring.
	age, waiting, _, _ := p.snapshot()
	progressAge.Record(context.Background(), age.Seconds(), attrs)

	stalled := p.stall() != nil
	if p.markReported(stalled) {
		if stalled {
			// The line the incident did not have. A wedged consumer stops logging entirely, so
			// this is the only record that will exist of what happened before the restart.
			c.logger.Error("consumer is stalled; liveness will fail until it recovers",
				"stream", p.stream, "consumer", p.consumer,
				"waiting", waiting, "since_progress", age.Round(time.Second).String())
			return
		}
		c.logger.Info("consumer recovered", "stream", p.stream, "consumer", p.consumer)
	}
}

// markPollReported is markReported's counterpart for poll failures.
func (p *consumerProgress) markPollReported(failing bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pollReported == failing {
		return false
	}
	p.pollReported = failing
	return true
}

// ConsumersProgressing reports whether every consumer this client started is either idle or
// making progress. It is the liveness question, and it is deliberately not the readiness one.
//
// Readiness removes a pod from Service endpoints. For a queue consumer that achieves close to
// nothing — no traffic is routed to dispatch or to a worker — so a stalled consumer would go on
// consuming nothing while looking correctly handled. Liveness restarts the container, which is
// what actually recovered the incident, and a queue consumer is the safest possible thing to
// restart: unacked messages are redelivered by JetStream, so the cost of being wrong is a few
// redeliveries, not lost work.
//
// Being wrong is still the risk worth designing against, which is why nothing here fails when the
// bus is unreachable: a check that turned a NATS blip into a fleet-wide restart would be a worse
// outage than the one it detects.
func (c *Client) ConsumersProgressing() error {
	c.mu.Lock()
	monitors := make([]*consumerProgress, len(c.progress))
	copy(monitors, c.progress)
	c.mu.Unlock()

	for _, p := range monitors {
		if err := p.stall(); err != nil {
			return err
		}
	}
	return nil
}

// progressAge is the metric half of this feature, and it is what alerting should use rather than
// the probe: the alert can fire *before* the restart threshold, giving an operator the chance to
// look at a live wedged pod instead of a restarted one.
var progressAge, _ = meter.Float64Gauge(
	"hermes.messaging.consumer.progress.age",
	metric.WithDescription("Seconds since a consumer last had evidence of health — a message finished, or a poll that found nothing waiting."),
	metric.WithUnit("s"),
)

// consumerLogger resolves the logger a client was built with.
func consumerLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	// A library that logs somewhere the service did not choose is worse than one that stays
	// quiet; bootstrap.MustConnectNATS passes the service's logger, so this is the test path.
	return slog.New(slog.DiscardHandler)
}
