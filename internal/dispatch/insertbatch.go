// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/observability"
)

// The dispatch write path was bounded by disk, not by dispatch. Measured throughput was ~1,242
// notifications/s on one replica and ~2,006/s on four, while Postgres used 2.5 of 24 cores;
// pg_test_fsync on the same volume returns 1,933 fdatasync ops/sec at 517us. With
// synchronous_commit on, a COMMIT cannot return until its WAL record is durable, so committing
// once per notification puts a hard ceiling at roughly one notification per fdatasync — which
// is where the measurement landed, within 4%.
//
// This file can amortise that flush: N notifications share one transaction, so the batch pays
// one fsync instead of N. It is opt-in, for reasons the second note below records. Everything
// here exists to do it without weakening the two guarantees the write path already made.
//
//   - At-least-once. A message is acked by internal/messaging only when its handler returns
//     nil, and Submit does not return until the row is committed (or its error is known). So
//     the ack still strictly follows durability, and a failed batch nacks every message in it.
//     Nothing is acked on the strength of a neighbour's success.
//   - Idempotency. handleSend has always tolerated "this notification already exists", because
//     a redelivery re-persists the same notification ID. That tolerance is preserved per row:
//     the batch INSERT skips conflicting rows instead of aborting, and the waiter for a skipped
//     row is told errAlreadyExists rather than being failed.

// How a batch is assembled — group commit, not a fixed window
//
// The obvious design is a timer: accumulate for X milliseconds, then write whatever arrived. It
// is also wrong here, and measurably so. Every producer is a worker blocked until its own row
// commits, so a linger of X caps the whole service at workers/X — 8 workers and a 5ms window is
// 1,600 notifications/s, *below* the throughput this change exists to raise. Benchmarked against
// the bundled Postgres (`cmd/dispatchbench`, 8 workers) that design came in at 2,274 msgs/s
// against an unbatched 2,459.
//
// So batches are assembled the way a database assembles a group commit instead. Take the row
// that woke the loop, take every row already queued behind it, write them, repeat. Nothing ever
// waits for a row that has not arrived, so:
//
//   - Latency is bounded by the flush in progress rather than by a configured window. A row that
//     arrives when the batcher is idle is written immediately — a quiet install pays nothing at
//     all, which no timer value can offer.
//   - Batch size is self-tuning. Rows accumulate only while a flush is in flight, so batches grow
//     exactly in proportion to how slow commits are: 1 when the disk is fast, up to the whole
//     worker pool when the disk is the bottleneck, which is the case this exists for.
//   - There is no throughput ceiling introduced by a waiting period, because there is none.
//
// insertLinger is the knob for the opposite trade — deliberately off by default. See its comment.

// Why batching itself is off by default
//
// Because the win could not be reproduced on the hardware available to the change, and the loss
// could. Batching converts N transactions that the database could have committed *in parallel*
// into one it must commit alone, which is a trade, not a free lunch: it pays off exactly when
// commit latency dominates and buys nothing when it does not.
//
// Measured with cmd/dispatchbench against the bundled Postgres, whose volume manages 10,582
// fdatasync/s at 94us — 5.5x faster than the 1,933/s at 517us on the volume where the ceiling
// was diagnosed, so the constraint this exists to lift is simply absent there:
//
//	 8 workers: 2,459 msgs/s unbatched, 2,163 batched  (-12%)
//	32 workers: 4,488 msgs/s unbatched, 4,432 batched  (within the confidence interval)
//
// On a volume 5.5x slower the arithmetic goes the other way, which is what the original
// measurement says: throughput sat within 4% of the disk's fdatasync rate. But "should" is not
// "did", so the mechanism ships ready and switched off, and the switch is one variable. Run
// `pg_test_fsync` on the target volume and `cmd/dispatchbench -insert-batch 16` against it; turn
// it on where the first number is small and the second is larger.
//
// Note also what it cannot fix: the insert is one of three commits dispatch makes per
// notification. EnsureUser upserts and UpdateNotificationChannels writes back the resolved
// channel set, and both are per-notification transactions of their own. Even a perfect batch
// removes a third of the flushes, so the honest expectation on a flush-bound host is around
// 1.3x, not Nx.
const (
	// defaultInsertBatchSize and defaultInsertLinger apply when no configuration is supplied —
	// the load-test harness, the e2e wiring, tests. A size of 1 is "no batching", per above.
	// cmd/dispatch overrides both from HERMES_DISPATCH_INSERT_BATCH_SIZE /
	// HERMES_DISPATCH_INSERT_BATCH_LINGER.
	defaultInsertBatchSize = 1
	defaultInsertLinger    = time.Duration(0)

	// insertFlushTimeout bounds one flush, and separately bounds the per-row retry that
	// follows a failed one. Deliberately well inside messaging's 30s HandlerTimeout: both can
	// run back to back, and the waiters should learn the batch's real error rather than each
	// independently hitting a deadline that tells them nothing about why.
	insertFlushTimeout = 10 * time.Second
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/dispatch")

var (
	// insertBatchRows is what says whether batching is buying anything. A mean near 1 means rows
	// are not piling up behind a flush — either the load is low, or commits are cheap enough on
	// this disk that there is nothing to amortise. A mean at the configured size means the size
	// is what is binding and can be raised (together with the worker count, which caps it).
	insertBatchRows, _ = meter.Int64Histogram(
		"hermes.dispatch.insert.batch.size",
		metric.WithDescription("Notifications persisted per batch transaction."),
		metric.WithUnit("1"),
	)

	// insertBatchFallbacks counts batches that failed and were re-inserted row by row. A
	// steady non-zero rate is the signature of a poison message landing in batch after batch
	// until it is dead-lettered; a spike across all batches is the database itself.
	insertBatchFallbacks, _ = meter.Int64Counter(
		"hermes.dispatch.insert.fallbacks",
		metric.WithDescription("Insert batches that failed and were retried one row at a time."),
		metric.WithUnit("1"),
	)
)

// errAlreadyExists reports that the row was skipped because a notification with this ID (or
// idempotency key) was already persisted. It is the batch path's equivalent of the duplicate-key
// error the single-row path returns, and handleSend treats the two identically.
var errAlreadyExists = errors.New("notification already exists")

// errBatcherStopped is returned to a caller that arrives after the batcher has shut down. It is
// transient by design: the message is nacked and redelivered to whichever replica is still
// serving, rather than being failed on the way out of a pod that is going away.
var errBatcherStopped = errors.New("insert batcher stopped")

// notificationInserter is the slice of the store the batcher needs. Narrower than
// store.NotificationRepository so the batcher can be tested against a few lines of fake.
type notificationInserter interface {
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	CreateNotifications(ctx context.Context, ns []*models.Notification) ([]string, error)
}

// insertRequest is one notification waiting for a transaction to carry it.
type insertRequest struct {
	// ctx is kept only to link the flush span back to the originating trace. The flush must
	// not run on it: it belongs to one message out of N, and its cancellation would take the
	// other N-1 down with it.
	ctx context.Context
	n   *models.Notification
	// result is buffered so the flusher can always deposit an answer and move on, even for a
	// caller that has already given up on its own context.
	result chan error
}

// insertBatcher collects notification inserts from the dispatch worker pool and writes them a
// transaction at a time. Callers block in Submit until their own row is accounted for.
type insertBatcher struct {
	store notificationInserter
	size  int
	// linger is how long an unfilled batch waits for more rows. Zero — the default — means it
	// does not wait at all; see the group-commit note at the top of this file.
	linger time.Duration
	logger *slog.Logger

	// in is unbuffered on purpose: a completed send means run has taken ownership of the
	// request and is now obliged to answer it. With a buffer, a request could sit in the
	// channel while run exits, and its caller would wait for an answer nobody owed it.
	in   chan *insertRequest
	stop chan struct{}
	// done is closed when run has exited. It is what releases a caller that arrives during
	// shutdown, and what stopAndWait waits on.
	done     chan struct{}
	stopOnce sync.Once
}

func newInsertBatcher(store notificationInserter, size int, linger time.Duration, logger *slog.Logger) *insertBatcher {
	if linger < 0 {
		linger = 0
	}
	return &insertBatcher{
		store:  store,
		size:   size,
		linger: linger,
		logger: logger,
		in:     make(chan *insertRequest),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Submit hands a notification to the batcher and blocks until it has been persisted, skipped as
// already present (errAlreadyExists), or definitively failed.
//
// Blocking is the design, not a compromise. The caller is a message handler whose return acks
// the message, so returning early would ack work that is still only in memory. Holding the
// worker costs nothing the pool cannot afford: it is parked on a channel, not on a database
// connection — it released its connection before it got here, and the flusher acquires exactly
// one for the whole batch.
func (b *insertBatcher) Submit(ctx context.Context, n *models.Notification) error {
	req := &insertRequest{ctx: ctx, n: n, result: make(chan error, 1)}

	select {
	case b.in <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return errBatcherStopped
	}

	select {
	case err := <-req.result:
		return err
	case <-ctx.Done():
		// The row may still be committed by the flush now in progress, and that is fine: this
		// message is nacked and redelivered, and the redelivery's insert conflicts with the
		// row this one left behind, which is the case the whole duplicate path exists for.
		// At-least-once is preserved in the direction that matters — nothing was acked.
		return ctx.Err()
	}
}

// run is the batcher's only goroutine: take a row, collect whatever else is waiting, write them,
// repeat. One transaction is open at a time and one database connection is used, no matter how
// many workers are feeding it.
//
// Writing inline rather than in a goroutine per batch is what makes the group commit above work
// at all — it is the flush occupying this loop that gives the next batch something to collect.
// It caps insert throughput at one batch per commit, which is the bound being amortised, not a
// new one: at 16 rows and a ~0.5ms commit that is ~32k notifications/s.
func (b *insertBatcher) run() {
	defer close(b.done)

	for {
		select {
		case req := <-b.in:
			// A batch is only ever assembled and written inside this one iteration, so the loop
			// holds nothing across iterations and shutdown has nothing to flush. Every request
			// collect takes is answered by the flush that follows it.
			b.flush(b.collect(req))
		case <-b.stop:
			return
		}
	}
}

// collect assembles the batch around the row that woke the loop: everything already queued
// behind it, and — only if a linger is configured — rows that turn up within that window.
func (b *insertBatcher) collect(first *insertRequest) []*insertRequest {
	batch := make([]*insertRequest, 1, b.size)
	batch[0] = first

	// The group-commit step, and the one that does the work. The hand-off channel is
	// unbuffered, so a receive succeeds here for exactly the producers already blocked trying
	// to hand a row over — the ones that piled up while the previous flush was running. Taking
	// them costs nothing and delays nobody.
	for len(batch) < b.size {
		select {
		case req := <-b.in:
			batch = append(batch, req)
		default:
			if b.linger > 0 {
				return b.waitForMore(batch)
			}
			return batch
		}
	}
	return batch
}

// waitForMore holds a partial batch open for up to the configured linger.
//
// Off unless an operator asks for it, because it trades away more than it looks like it does:
// the callers it makes wait are worker-pool goroutines that cannot start another notification
// while they wait, so a linger of X puts a ceiling of workers/X on the whole service. It exists
// for the case where that ceiling is comfortably above the target rate and the extra rows per
// transaction are worth more — a very slow disk with a large worker pool.
func (b *insertBatcher) waitForMore(batch []*insertRequest) []*insertRequest {
	timer := time.NewTimer(b.linger)
	defer timer.Stop()

	for len(batch) < b.size {
		select {
		case req := <-b.in:
			batch = append(batch, req)
		case <-timer.C:
			return batch
		case <-b.stop:
			// Shutting down: write what we have rather than making these callers sit out the
			// rest of the window. Nothing is consumed here — the channel is closed, so run's
			// own select still sees the stop on the next pass.
			return batch
		}
	}
	return batch
}

// stopAndWait shuts the batcher down and waits for the final flush. Callers that arrive
// afterwards get errBatcherStopped and are retried elsewhere.
func (b *insertBatcher) stopAndWait() {
	b.stopOnce.Do(func() { close(b.stop) })
	<-b.done
}

// flush writes one batch and answers every caller in it — exactly once each, on every path.
func (b *insertBatcher) flush(reqs []*insertRequest) {
	if len(reqs) == 0 {
		return
	}

	// Links, not a parent: this span has N originating traces and belongs to none of them.
	// Same reasoning (and same shape) as eventwriter.flush.
	links := make([]trace.Link, 0, len(reqs))
	notifications := make([]*models.Notification, len(reqs))
	for i, r := range reqs {
		notifications[i] = r.n
		if sc := trace.SpanContextFromContext(r.ctx); sc.IsValid() {
			links = append(links, trace.Link{SpanContext: sc})
		}
	}

	// context.Background, not a caller's context: one message's cancellation — its handler
	// timing out, its pod draining — must not roll back the transaction carrying N-1 others.
	ctx, cancel := context.WithTimeout(context.Background(), insertFlushTimeout)
	defer cancel()

	tracer := otel.Tracer("github.com/hermesnotifications/hermes/internal/dispatch")
	ctx, span := tracer.Start(ctx, "dispatch.insert_batch",
		trace.WithLinks(links...),
		trace.WithAttributes(attribute.Int("batch.size", len(reqs))),
	)
	defer span.End()

	insertBatchRows.Record(ctx, int64(len(reqs)))

	inserted, err := b.store.CreateNotifications(ctx, notifications)
	if err != nil {
		observability.RecordError(span, err)
		b.insertIndividually(reqs, err)
		return
	}

	// Everything not reported as inserted was skipped as already present. Told apart rather
	// than lumped in with success so the "already exists (retry), continuing" path stays
	// visible in the logs, exactly as it was when each insert had its own statement.
	insertedIDs := make(map[string]struct{}, len(inserted))
	for _, id := range inserted {
		insertedIDs[id] = struct{}{}
	}
	for _, r := range reqs {
		if _, ok := insertedIDs[r.n.ID]; ok {
			r.result <- nil
			continue
		}
		r.result <- errAlreadyExists
	}
}

// insertIndividually re-inserts a failed batch one row at a time, so each caller learns its own
// fate instead of inheriting the batch's.
//
// This is what bounds the blast radius of a poison message. A row the database will never
// accept — a user_id that violates the foreign key, an unparseable value — aborts the whole
// transaction, and without this every message batched alongside it would be nacked, regrouped
// with other innocent messages on redelivery, and fail again. The retry separates the two
// cases at a cost of one extra pass: if the database is down, every row fails again and every
// message is retried, which is correct; if one row is poison, only that row fails and its N-1
// neighbours commit and ack. The poison message is then retried on its own merits and
// dead-lettered by internal/messaging after MaxDeliver attempts, as it was before batching.
func (b *insertBatcher) insertIndividually(reqs []*insertRequest, batchErr error) {
	insertBatchFallbacks.Add(context.Background(), 1)
	b.logger.Warn("insert batch failed; retrying rows individually",
		"error", batchErr, "rows", len(reqs))

	// A fresh budget rather than what is left of the batch's: if the batch failed *because* it
	// ran out of time, reusing that context would fail every row on arrival and turn one slow
	// commit into N pointless redeliveries.
	ctx, cancel := context.WithTimeout(context.Background(), insertFlushTimeout)
	defer cancel()

	for _, r := range reqs {
		_, err := b.store.CreateNotification(ctx, r.n)
		r.result <- err
	}
}
