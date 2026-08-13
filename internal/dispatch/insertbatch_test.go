// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/models"
)

// These tests pin the two properties batching must not cost us — a message is only acked once its
// row is committed, and one duplicate or one poison row cannot fail its neighbours — and the way
// batches are assembled, which is where the throughput comes from and what a future edit is most
// likely to get wrong. They drive the batcher directly rather than through NATS: the ack decision
// lives in internal/messaging and keys off nothing but the error returned here.

// --- fakes -------------------------------------------------------------------------------

type fakeInserter struct {
	mu      sync.Mutex
	batches [][]string // notification IDs per CreateNotifications call
	singles []string   // notification IDs per CreateNotification call

	// skip lists IDs the batch insert reports as already present (ON CONFLICT DO NOTHING).
	skip map[string]bool
	// batchErr fails every CreateNotifications call, forcing the per-row fallback.
	batchErr error
	// rowErr fails a specific ID's single-row insert.
	rowErr map[string]error
	// block, when non-nil, holds CreateNotifications until it is closed. entered, when
	// non-nil, is signalled on the way in — together they let a test pin a flush open and know
	// that it is open.
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeInserter) CreateNotifications(_ context.Context, ns []*models.Notification) ([]string, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(ns))
	for _, n := range ns {
		ids = append(ids, n.ID)
	}
	f.batches = append(f.batches, ids)
	if f.batchErr != nil {
		return nil, f.batchErr
	}

	inserted := make([]string, 0, len(ns))
	for _, n := range ns {
		if f.skip[n.ID] {
			continue
		}
		inserted = append(inserted, n.ID)
	}
	return inserted, nil
}

func (f *fakeInserter) CreateNotification(_ context.Context, n *models.Notification) (*models.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.singles = append(f.singles, n.ID)
	if err := f.rowErr[n.ID]; err != nil {
		return nil, err
	}
	return n, nil
}

func (f *fakeInserter) batchSizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	sizes := make([]int, len(f.batches))
	for i, b := range f.batches {
		sizes[i] = len(b)
	}
	return sizes
}

func (f *fakeInserter) singleInserts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.singles...)
}

// noLinger is the production setting: batches are assembled from rows already waiting, never by
// holding one open. Named so the tests that use it say which mode they are exercising.
const noLinger = time.Duration(0)

// testLinger is long enough that a batch always fills before it expires, so the tests that pin
// batch composition are deterministic rather than racing a clock. waitForMore returns as soon as
// the batch is full, so nothing actually waits this long.
const testLinger = 2 * time.Second

func testBatcher(t *testing.T, store notificationInserter, size int, linger time.Duration) *insertBatcher {
	t.Helper()
	b := newInsertBatcher(store, size, linger, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go b.run()
	t.Cleanup(b.stopAndWait)
	return b
}

// submitAll submits one notification per ID concurrently and returns each one's error, keyed by
// ID. Concurrent because that is how the worker pool uses the batcher, and because a batch can
// only fill if its rows are in flight at the same time.
func submitAll(t *testing.T, b *insertBatcher, ids ...string) map[string]error {
	t.Helper()

	var mu sync.Mutex
	errs := make(map[string]error, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := b.Submit(ctx, &models.Notification{ID: id})
			mu.Lock()
			errs[id] = err
			mu.Unlock()
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Submit did not return; a caller was left waiting for an answer nobody owed it")
	}
	return errs
}

// --- tests -------------------------------------------------------------------------------

// The mechanism the throughput comes from, with no linger configured: rows that pile up while a
// transaction is committing are written by the next one, together. Nobody waited for them —
// they were already there.
func TestInsertBatcher_GroupsRowsThatPileUpDuringAFlush(t *testing.T) {
	store := &fakeInserter{block: make(chan struct{}), entered: make(chan struct{}, 4)}
	b := testBatcher(t, store, 8, noLinger)

	first := make(chan error, 1)
	go func() { first <- b.Submit(context.Background(), &models.Notification{ID: "n1"}) }()

	// n1's flush is now in progress and holding the batcher.
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first row was never flushed")
	}

	// n2 and n3 arrive while it commits, and block handing over. They are what the next batch
	// is assembled from.
	rest := make(chan error, 2)
	for _, notifID := range []string{"n2", "n3"} {
		go func() { rest <- b.Submit(context.Background(), &models.Notification{ID: notifID}) }()
	}
	time.Sleep(50 * time.Millisecond)

	close(store.block)
	for range 3 {
		select {
		case err := <-first:
			if err != nil {
				t.Fatalf("Submit(n1) = %v, want nil", err)
			}
		case err := <-rest:
			if err != nil {
				t.Fatalf("Submit = %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a caller was left waiting")
		}
	}

	got := store.batchSizes()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("batch sizes = %v, want [1 2]: the two rows that queued behind the first "+
			"flush should share the transaction that follows it", got)
	}
}

// The other half of that bargain: with nothing waiting, a row is written immediately rather than
// held for company. A quiet install pays nothing for batching being switched on.
func TestInsertBatcher_WritesALoneRowWithoutWaiting(t *testing.T) {
	store := &fakeInserter{}
	b := testBatcher(t, store, 100, noLinger)

	started := time.Now()
	if errs := submitAll(t, b, "n1"); errs["n1"] != nil {
		t.Fatalf("Submit = %v, want nil", errs["n1"])
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Errorf("a lone row took %s to be written; it should not have waited for anything", elapsed)
	}
	if got := store.batchSizes(); len(got) != 1 || got[0] != 1 {
		t.Errorf("batch sizes = %v, want one batch of 1", got)
	}
}

// The opt-in linger holds an unfilled batch open for more rows — the trade this defaults away
// from, since every waiting row is a worker that cannot start another notification.
func TestInsertBatcher_LingerFillsAPartialBatch(t *testing.T) {
	store := &fakeInserter{}
	b := testBatcher(t, store, 3, testLinger)

	errs := submitAll(t, b, "n1", "n2", "n3")
	for id, err := range errs {
		if err != nil {
			t.Errorf("Submit(%s) = %v, want nil", id, err)
		}
	}
	if got := store.batchSizes(); len(got) != 1 || got[0] != 3 {
		t.Errorf("batch sizes = %v, want one batch of 3", got)
	}
	if got := store.singleInserts(); len(got) != 0 {
		t.Errorf("single-row inserts = %v, want none", got)
	}
}

// Idempotency, preserved per row. A redelivered message re-persists a notification ID that is
// already there; the batch skips that row and continues, and only that caller hears about it.
func TestInsertBatcher_DuplicateDoesNotFailItsNeighbours(t *testing.T) {
	store := &fakeInserter{skip: map[string]bool{"n2": true}}
	b := testBatcher(t, store, 3, testLinger)

	errs := submitAll(t, b, "n1", "n2", "n3")
	if !errors.Is(errs["n2"], errAlreadyExists) {
		t.Errorf("Submit(n2) = %v, want errAlreadyExists", errs["n2"])
	}
	if errs["n1"] != nil || errs["n3"] != nil {
		t.Errorf("neighbours failed alongside the duplicate: n1=%v n3=%v", errs["n1"], errs["n3"])
	}
	if got := store.batchSizes(); len(got) != 1 {
		t.Errorf("batch count = %d, want 1 — a duplicate must not trigger the fallback", len(got))
	}
	// And handleSend must read that sentinel as "carry on", not as a failure.
	if !isDuplicateNotification(errAlreadyExists) {
		t.Error("isDuplicateNotification does not recognise errAlreadyExists")
	}
}

// Blast radius. One row the database will never accept fails the transaction it is in; the
// fallback re-inserts each row on its own so the message carrying the poison row is the only one
// nacked, and the rest commit and ack.
func TestInsertBatcher_PoisonRowFailsAloneAfterTheFallback(t *testing.T) {
	poison := errors.New("insert or update on table \"notifications\" violates foreign key constraint")
	store := &fakeInserter{
		batchErr: poison,
		rowErr:   map[string]error{"n2": poison},
	}
	b := testBatcher(t, store, 3, testLinger)

	errs := submitAll(t, b, "n1", "n2", "n3")
	if !errors.Is(errs["n2"], poison) {
		t.Errorf("Submit(n2) = %v, want the poison row's own error", errs["n2"])
	}
	if errs["n1"] != nil || errs["n3"] != nil {
		t.Errorf("innocent rows failed with the poison row: n1=%v n3=%v", errs["n1"], errs["n3"])
	}
	if got := store.singleInserts(); len(got) != 3 {
		t.Errorf("single-row retries = %v, want all three rows retried individually", got)
	}
}

// The other half of the fallback: when it is the database that is unavailable rather than one
// row, every caller must fail, because every message must be redelivered. Silence here would be
// an ack for a row that was never written.
func TestInsertBatcher_DatabaseOutageFailsEveryCaller(t *testing.T) {
	down := errors.New("connection refused")
	store := &fakeInserter{
		batchErr: down,
		rowErr:   map[string]error{"n1": down, "n2": down, "n3": down},
	}
	b := testBatcher(t, store, 3, testLinger)

	for id, err := range submitAll(t, b, "n1", "n2", "n3") {
		if !errors.Is(err, down) {
			t.Errorf("Submit(%s) = %v, want the outage error so the message is nacked", id, err)
		}
	}
}

// A caller whose handler times out mid-flush must be released, and must not leave the flusher
// blocked trying to hand it an answer — that would wedge the batcher for every message after it.
func TestInsertBatcher_CallerGivingUpDoesNotWedgeTheFlusher(t *testing.T) {
	store := &fakeInserter{block: make(chan struct{})}
	b := testBatcher(t, store, 1, noLinger)

	ctx, cancel := context.WithCancel(context.Background())
	abandoned := make(chan error, 1)
	go func() { abandoned <- b.Submit(ctx, &models.Notification{ID: "n1"}) }()

	// Let the flush start, then take the caller away from under it.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-abandoned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Submit did not return after its context was cancelled")
	}

	// The flusher must survive the abandonment and keep serving. (Closed rather than set back
	// to nil: the batcher goroutine reads this field, so swapping it here would be a race.)
	close(store.block)
	if errs := submitAll(t, b, "n2"); errs["n2"] != nil {
		t.Fatalf("Submit after an abandoned caller = %v, want nil", errs["n2"])
	}
}

// A row already held open by a linger must be written when the batcher stops, not made to sit
// out the rest of the window. Its caller is a worker holding an unacked message, and answering it
// is what lets the NATS drain converge instead of timing out and redelivering work that was one
// commit from done.
func TestInsertBatcher_StopWritesARowHeldByTheLinger(t *testing.T) {
	store := &fakeInserter{}
	// A linger far longer than the test: only the stop can end it.
	b := newInsertBatcher(store, 100, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go b.run()

	result := make(chan error, 1)
	go func() { result <- b.Submit(context.Background(), &models.Notification{ID: "n1"}) }()

	// The hand-off is over an unbuffered channel, so its completion is not observable from here;
	// a pause is the honest way to say "let the batcher take the row". If the pause is ever too
	// short the assertions below fail loudly rather than passing for another reason.
	time.Sleep(50 * time.Millisecond)
	b.stopAndWait()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Submit = %v, want nil — a held row must be written, not failed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Submit never returned across shutdown")
	}
	if got := store.batchSizes(); len(got) != 1 || got[0] != 1 {
		t.Errorf("batch sizes = %v, want one batch of 1 written on the way out", got)
	}
}

// A caller that arrives after shutdown gets a retryable error rather than blocking forever on a
// batcher that will never answer.
func TestInsertBatcher_SubmitAfterStopIsRetryable(t *testing.T) {
	b := newInsertBatcher(&fakeInserter{}, 4, noLinger, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go b.run()
	b.stopAndWait()

	err := b.Submit(context.Background(), &models.Notification{ID: "n1"})
	if !errors.Is(err, errBatcherStopped) {
		t.Fatalf("Submit after stop = %v, want errBatcherStopped", err)
	}
	if isDuplicateNotification(err) {
		t.Error("errBatcherStopped reads as a duplicate; it would be swallowed instead of retried")
	}
}

// The effective batch size cannot exceed the worker count: every row in a batch is a worker
// blocked waiting for it. A size above that would only ever be reached by the timer expiring.
func TestStartInsertBatcher_CapsSizeAtTheWorkerCount(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		workers    int
		wantSize   int // 0 means "no batcher at all"
	}{
		{name: "capped by a small pool", configured: 16, workers: 4, wantSize: 4},
		{name: "configured size below the pool", configured: 4, workers: 16, wantSize: 4},
		{name: "single worker leaves nothing to batch", configured: 16, workers: 1, wantSize: 0},
		{name: "explicitly disabled", configured: 1, workers: 16, wantSize: 0},
		{name: "disabled by a zero", configured: 0, workers: 16, wantSize: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &Dispatch{
				store:           &fakeNotifStore{},
				logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
				insertBatchSize: tc.configured,
				insertLinger:    noLinger,
			}
			d.startInsertBatcher(tc.workers)
			t.Cleanup(d.Stop)

			if tc.wantSize == 0 {
				if d.inserts != nil {
					t.Fatalf("batcher created with size %d, want none", d.inserts.size)
				}
				return
			}
			if d.inserts == nil {
				t.Fatal("no batcher created")
			}
			if d.inserts.size != tc.wantSize {
				t.Errorf("batch size = %d, want %d", d.inserts.size, tc.wantSize)
			}
		})
	}
}

// A negative linger is a misconfiguration, not an instruction to wait forever.
func TestNewInsertBatcher_NormalisesANegativeLinger(t *testing.T) {
	b := newInsertBatcher(&fakeInserter{}, 4, -time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if b.linger != 0 {
		t.Fatalf("linger = %v, want 0", b.linger)
	}
}

// End to end through the handler: a notification the batch reports as already present is not a
// failure. handleSend must carry on and fan out, exactly as it did when the duplicate arrived as
// a unique-violation from a single-row insert.
func TestHandleSend_BatchedDuplicateStillFansOut(t *testing.T) {
	bus := &fakeBus{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, &fakeNotifStore{}, user)
	// "ntf_test" is the ID directSend mints; report it as already persisted.
	d.inserts = testBatcher(t, &fakeInserter{skip: map[string]bool{"ntf_test": true}}, 1, noLinger)

	if err := d.handleSend(context.Background(), directSend("email", "inbox"), firstAttempt()); err != nil {
		t.Fatalf("handleSend on a duplicate = %v, want nil", err)
	}
	if got := bus.deliverySubjects(); len(got) != 2 {
		t.Errorf("delivery publishes = %v, want both channels fanned out", got)
	}
}

// The reverse: a batch that could not be written must fail the handler, so the message is nacked
// and redelivered rather than acked on the strength of a row that does not exist.
func TestHandleSend_FailedBatchIsRetried(t *testing.T) {
	bus := &fakeBus{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, &fakeNotifStore{}, user)
	down := errors.New("connection refused")
	d.inserts = testBatcher(t, &fakeInserter{
		batchErr: down,
		rowErr:   map[string]error{"ntf_test": down},
	}, 1, noLinger)

	err := d.handleSend(context.Background(), directSend("email"), firstAttempt())
	if err == nil {
		t.Fatal("handleSend = nil on an unwritten notification; the message would be acked")
	}
	var perm interface{ Permanent() bool }
	if errors.As(err, &perm) && perm.Permanent() {
		t.Errorf("error is permanent (%v); a database outage must be retried", err)
	}
	if got := bus.deliverySubjects(); len(got) != 0 {
		t.Errorf("fanned out %v despite failing to persist", got)
	}
}
