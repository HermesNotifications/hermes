// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DispatchFactory builds and starts a dispatch consumer for one repetition with
// the given worker/prefetch config, returning a stop func that fully tears it
// down (Close on a fresh messaging.Client). Implemented in cmd/dispatchbench.
type DispatchFactory func(workers, prefetch int) (stop func(), err error)

// Publisher publishes n synthetic notification.send messages for the given
// backend. Implemented in cmd/dispatchbench (chooses pg/dynamo recipients).
type Publisher func(ctx context.Context, n int) error

// Resetter clears state between repetitions: purge streams, delete the dispatch
// consumer, and truncate bench rows. Implemented in cmd/dispatchbench.
type Resetter func(ctx context.Context) error

// Runner measures one cell's repetitions.
type Runner struct {
	JS       jetstream.JetStream
	Stream   string // "NOTIFICATIONS"
	Consumer string // "dispatch"
	N        int    // messages per drain
	Publish  Publisher
	Dispatch DispatchFactory
	Reset    Resetter
	Poll     time.Duration // consumer-info poll interval, e.g. 50ms
}

// Drain runs one repetition and returns throughput in msgs/sec. ctx SHOULD carry
// a deadline: if dispatch stalls (e.g. a consumer wedged with in-flight messages)
// waitDrained blocks until ctx is cancelled. The harness passes a per-drain timeout.
func (r *Runner) Drain(ctx context.Context, cell Cell) (float64, error) {
	if err := r.Reset(ctx); err != nil {
		return 0, fmt.Errorf("reset: %w", err)
	}
	if err := r.Publish(ctx, r.N); err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}
	start := time.Now()
	stop, err := r.Dispatch(cell.Workers, cell.Prefetch)
	if err != nil {
		return 0, fmt.Errorf("start dispatch: %w", err)
	}
	defer stop()

	if err := r.waitDrained(ctx); err != nil {
		return 0, err
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, fmt.Errorf("non-positive elapsed time")
	}
	return float64(r.N) / elapsed, nil
}

// waitDrained polls the consumer until pending (NumPending+NumAckPending) hits 0,
// requiring at least one observation of pending>0 first so we never accept the
// pre-consumer-creation state as "done".
func (r *Runner) waitDrained(ctx context.Context) error {
	ticker := time.NewTicker(r.Poll)
	defer ticker.Stop()
	sawWork := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			cons, err := r.JS.Consumer(ctx, r.Stream, r.Consumer)
			if err != nil {
				continue // consumer not created yet
			}
			info, err := cons.Info(ctx)
			if err != nil {
				continue
			}
			pending := info.NumPending + uint64(info.NumAckPending)
			if pending > 0 {
				sawWork = true
			} else if sawWork {
				return nil
			}
		}
	}
}
