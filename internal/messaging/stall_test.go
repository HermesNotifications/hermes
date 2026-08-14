// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// The four cases that decide whether this check is safe to attach to a liveness probe. Each one
// is a state the fleet is genuinely in at some point, and three of the four must stay healthy —
// only one may ever restart a pod.
//
// Driven through a fake clock rather than through sleeps: the production window is ten minutes,
// and a test that waited it out would either be skipped or shortened until it proved something
// other than what it claims to.

// fakeClock is a settable now for consumerProgress.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestProgress builds a monitor on a fake clock with a one-minute window.
func newTestProgress(t *testing.T) (*consumerProgress, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	p := newConsumerProgress("NOTIFICATIONS", "dispatch", nil, time.Minute)
	p.now = clock.now
	p.lastEvidence = clock.now()
	return p, clock
}

// An empty queue produces no progress at all, forever, and that is the normal state of a
// correctly working consumer at 3am. If this case ever fails, the probe restarts the fleet every
// quiet period — which is why "pending is zero" is evidence of health rather than the absence of
// it.
func TestConsumerProgress_IdleConsumerStaysHealthy(t *testing.T) {
	p, clock := newTestProgress(t)

	for range 100 {
		clock.advance(time.Minute)
		p.observe(0, nil)
		if err := p.stall(); err != nil {
			t.Fatalf("idle consumer reported stalled: %v", err)
		}
	}
}

// A large backlog being worked through slowly. The window is a minute and the consumer finishes
// one message every 50 seconds — miserable throughput, and not a liveness problem: the loop is
// turning, so a restart would only throw away the messages in flight.
func TestConsumerProgress_SlowButMovingStaysHealthy(t *testing.T) {
	p, clock := newTestProgress(t)

	for range 20 {
		clock.advance(50 * time.Second)
		p.observe(133472, nil)
		if err := p.stall(); err != nil {
			t.Fatalf("draining consumer reported stalled: %v", err)
		}
		p.finished()
	}
}

// The incident. Work waiting, nothing taken, and no amount of waiting changes it.
func TestConsumerProgress_BacklogWithNoProgressStalls(t *testing.T) {
	p, clock := newTestProgress(t)

	clock.advance(59 * time.Second)
	p.observe(133472, nil)
	if err := p.stall(); err != nil {
		t.Fatalf("reported stalled inside the window: %v", err)
	}

	clock.advance(2 * time.Second)
	p.observe(133472, nil)
	err := p.stall()
	if err == nil {
		t.Fatal("a consumer holding 133472 messages and finishing none was reported healthy")
	}
	// The message is what an operator reads in `kubectl describe`, so it has to name the consumer
	// and the size of the backlog rather than just saying "unhealthy".
	for _, want := range []string{"dispatch", "NOTIFICATIONS", "133472"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("stall message %q does not mention %q", err, want)
		}
	}

	// And it recovers on its own the moment a message completes — no restart needed if the
	// consumer unwedges itself between the probe and the kubelet's failure threshold.
	p.finished()
	if err := p.stall(); err != nil {
		t.Fatalf("still stalled after a message completed: %v", err)
	}
}

// Ack-pending counts as work waiting. Every worker wedged inside a handler with an empty stream
// behind them is the same failure one degree luckier, and a NumPending-only check would call it
// idle forever. Subscribe passes NumPending+NumAckPending for exactly this reason.
func TestConsumerProgress_AckPendingOnlyBacklogStalls(t *testing.T) {
	p, clock := newTestProgress(t)

	clock.advance(2 * time.Minute)
	p.observe(256, nil) // nothing pending in the stream; 256 messages held by wedged handlers
	if err := p.stall(); err == nil {
		t.Fatal("a consumer holding only ack-pending work was reported healthy")
	}
}

// A bus that cannot be reached is the case that decides whether this check is worth having. Every
// consumer pod in the fleet reaches the same conclusion at the same moment, so if "I cannot tell"
// counted as a stall, a NATS blip would restart the entire pipeline — a far worse outage than the
// one being detected.
func TestConsumerProgress_UnreachableBusNeverStalls(t *testing.T) {
	p, clock := newTestProgress(t)

	// Backlog first, so there is real stall evidence to discard.
	p.observe(5000, nil)
	clock.advance(59 * time.Second)

	for range 100 {
		clock.advance(time.Minute)
		p.observe(0, errNotConnected)
		if err := p.stall(); err != nil {
			t.Fatalf("reported stalled while the bus was unreachable: %v", err)
		}
	}

	// And the window starts over on recovery rather than tripping on the first poll back.
	p.observe(5000, nil)
	if err := p.stall(); err != nil {
		t.Fatalf("reported stalled on the first poll after reconnecting: %v", err)
	}
	clock.advance(61 * time.Second)
	p.observe(5000, nil)
	if err := p.stall(); err == nil {
		t.Fatal("never stalls again after a reconnect — the outage grace period is permanent")
	}
}

// A backlog that arrives after a long quiet period must get its own full window. Without this the
// stall clock would still be running from whenever the queue last emptied, and the first message
// after an idle night would look like hours of no progress.
func TestConsumerProgress_BacklogAfterIdlePeriodGetsAFullWindow(t *testing.T) {
	p, clock := newTestProgress(t)

	for range 60 {
		clock.advance(time.Minute)
		p.observe(0, nil)
	}

	p.observe(50000, nil)
	if err := p.stall(); err != nil {
		t.Fatalf("a backlog arriving after an idle hour was immediately stalled: %v", err)
	}
}

// Draining is not stalling. A pod told to shut down stops taking work on purpose, and the drain
// budget (30s by default) can outlast a short stall window.
func TestConsumerProgress_StoppedMonitorReportsHealthy(t *testing.T) {
	p, clock := newTestProgress(t)

	clock.advance(10 * time.Minute)
	p.observe(1000, nil)
	if err := p.stall(); err == nil {
		t.Fatal("expected a stall before stopping, so the next assertion means something")
	}

	p.stop()
	if err := p.stall(); err != nil {
		t.Fatalf("a draining consumer reported stalled: %v", err)
	}
}

// The escape hatch: HERMES_NATS_CONSUMER_STALL_TIMEOUT=0 turns the whole thing off, and must not
// half-work.
func TestConsumerProgress_ZeroTimeoutDisablesDetection(t *testing.T) {
	clock := newFakeClock()
	p := newConsumerProgress("NOTIFICATIONS", "dispatch", nil, 0)
	p.now = clock.now
	p.lastEvidence = clock.now()

	clock.advance(24 * time.Hour)
	p.observe(1_000_000, nil)
	if err := p.stall(); err != nil {
		t.Fatalf("detection is disabled but reported: %v", err)
	}
}

// The transition debounce, which is what keeps a wedged consumer from writing the same line every
// 30s for three hours — the log volume the runbook has to read through.
func TestConsumerProgress_LogsOnTransitionsOnly(t *testing.T) {
	p, _ := newTestProgress(t)

	if !p.markReported(true) {
		t.Fatal("the first stall should be reported")
	}
	if p.markReported(true) {
		t.Fatal("a continuing stall should not be reported again")
	}
	if !p.markReported(false) {
		t.Fatal("recovery should be reported")
	}
}

func TestClient_ConsumersProgressing(t *testing.T) {
	clock := newFakeClock()

	healthy := newConsumerProgress("DELIVERY", "worker-email", nil, time.Minute)
	healthy.now = clock.now
	healthy.lastEvidence = clock.now()

	stalled := newConsumerProgress("NOTIFICATIONS", "dispatch", nil, time.Minute)
	stalled.now = clock.now
	stalled.lastEvidence = clock.now()

	c := &Client{}
	if err := c.ConsumersProgressing(); err != nil {
		t.Fatalf("a client with no consumers is not stalled, got: %v", err)
	}

	c.progress = []*consumerProgress{healthy, stalled}
	if err := c.ConsumersProgressing(); err != nil {
		t.Fatalf("both consumers are fresh, got: %v", err)
	}

	clock.advance(2 * time.Minute)
	healthy.finished()
	stalled.observe(99, nil)

	err := c.ConsumersProgressing()
	if err == nil {
		t.Fatal("one stalled consumer must fail the whole check — the pod cannot do its job")
	}
	if !strings.Contains(err.Error(), "dispatch") {
		t.Errorf("the failing consumer should be named, got: %v", err)
	}
}

// A stall must survive a poll that could not answer *and* be reachable again afterwards; the
// error path in observe resets the clock, and getting that backwards would make the check either
// permanently blind or permanently angry.
func TestConsumerProgress_PollErrorResetsTheWindow(t *testing.T) {
	p, clock := newTestProgress(t)

	p.observe(10, nil)
	clock.advance(59 * time.Second)
	p.observe(0, errors.New("timeout"))
	clock.advance(30 * time.Second)
	p.observe(10, nil)
	if err := p.stall(); err != nil {
		t.Fatalf("a poll failure should restart the window, got: %v", err)
	}
}

func TestPollIntervalFor(t *testing.T) {
	cases := []struct {
		timeout time.Duration
		want    time.Duration
	}{
		// The production default samples at the 30s ceiling: ten samples per window would be one
		// per minute, which is slower than it needs to be for free.
		{DefaultConsumerStallTimeout, 30 * time.Second},
		{2 * time.Minute, 12 * time.Second},
		{time.Second, 100 * time.Millisecond},
		// A pathologically small window must not spin the poller.
		{time.Millisecond, minConsumerPollInterval},
	}
	for _, tc := range cases {
		if got := pollIntervalFor(tc.timeout); got != tc.want {
			t.Errorf("pollIntervalFor(%s) = %s, want %s", tc.timeout, got, tc.want)
		}
	}
}
