// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package eventwriter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/models"
	hermenats "github.com/hermesnotifications/hermes/internal/nats"
	"github.com/hermesnotifications/hermes/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// fakeEventStore is a hand-written EventRepository; only the two batch methods
// the writer calls do anything.
type fakeEventStore struct {
	insertErr error
	inserted  []models.NotificationEvent
	updates   []store.StatusUpdate
}

func (f *fakeEventStore) InsertEvents(_ context.Context, events []models.NotificationEvent) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, events...)
	return nil
}

func (f *fakeEventStore) BatchUpdateNotificationStatuses(_ context.Context, updates []store.StatusUpdate) error {
	f.updates = append(f.updates, updates...)
	return nil
}

func (f *fakeEventStore) InsertEvent(context.Context, string, string, string, string, []byte) error {
	return nil
}

func (f *fakeEventStore) UpdateNotificationStatus(context.Context, string, models.NotificationStatus, time.Time) error {
	return nil
}

func (f *fakeEventStore) DeleteEventsOlderThan(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func newRecordingTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

// ctxOnItsOwnTrace returns a context carrying a live span, standing in for the
// nats.consume span that the event originally arrived under.
func ctxOnItsOwnTrace(t *testing.T, name string) context.Context {
	t.Helper()
	ctx, span := otel.Tracer("test").Start(context.Background(), name)
	t.Cleanup(func() { span.End() })
	return ctx
}

func testWriter(st store.EventRepository) *Writer {
	return &Writer{
		store:  st,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func spansNamed(sr *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// The regression test for the defect this package shipped with: every event
// write landed in a trace of its own, disconnected from the notification that
// caused it, because flush started from context.Background() and recorded the
// originating traces only as links.
func TestFlush_PersistSpanLandsOnTheOriginatingTrace(t *testing.T) {
	sr := newRecordingTracer(t)
	st := &fakeEventStore{}
	w := testWriter(st)

	ctxA := ctxOnItsOwnTrace(t, "consume.a")
	ctxB := ctxOnItsOwnTrace(t, "consume.b")
	traceA := trace.SpanContextFromContext(ctxA).TraceID()
	traceB := trace.SpanContextFromContext(ctxB).TraceID()
	if traceA == traceB {
		t.Fatal("precondition: the two items must be on different traces")
	}

	w.flush([]BatchItem[*hermenats.EventMessage]{
		{Ctx: ctxA, Msg: &hermenats.EventMessage{NotificationID: "n_a", Channel: "inbox", Event: "inbox.sent"}},
		{Ctx: ctxB, Msg: &hermenats.EventMessage{NotificationID: "n_b", Channel: "email", Event: "email.sent"}},
	})

	persisted := spansNamed(sr, "notification.event.persist")
	if len(persisted) != 2 {
		t.Fatalf("got %d notification.event.persist spans, want 2", len(persisted))
	}

	byTrace := map[trace.TraceID]sdktrace.ReadOnlySpan{}
	for _, s := range persisted {
		byTrace[s.SpanContext().TraceID()] = s
	}
	if _, ok := byTrace[traceA]; !ok {
		t.Errorf("no persist span on trace A (%s); the event write is still orphaned", traceA)
	}
	if _, ok := byTrace[traceB]; !ok {
		t.Errorf("no persist span on trace B (%s); the event write is still orphaned", traceB)
	}

	// The batch span must stay a root on its own trace -- it belongs to no single
	// notification, and reparenting it onto one of them would be a lie.
	flushes := spansNamed(sr, "eventwriter.flush")
	if len(flushes) != 1 {
		t.Fatalf("got %d eventwriter.flush spans, want 1", len(flushes))
	}
	if flushes[0].SpanContext().TraceID() == traceA || flushes[0].SpanContext().TraceID() == traceB {
		t.Error("flush span was reparented onto an item trace; it should stay its own root")
	}
	if n := len(flushes[0].Links()); n != 2 {
		t.Errorf("flush span has %d links, want 2 (one per item)", n)
	}
}

// Each per-item span links back to the batch, so the batch is reachable from any
// one notification's trace.
func TestFlush_PersistSpanLinksBackToTheBatch(t *testing.T) {
	sr := newRecordingTracer(t)
	w := testWriter(&fakeEventStore{})

	ctx := ctxOnItsOwnTrace(t, "consume")
	w.flush([]BatchItem[*hermenats.EventMessage]{
		{Ctx: ctx, Msg: &hermenats.EventMessage{NotificationID: "n_a", Channel: "inbox", Event: "inbox.sent"}},
	})

	persisted := spansNamed(sr, "notification.event.persist")
	if len(persisted) != 1 {
		t.Fatalf("got %d persist spans, want 1", len(persisted))
	}
	flushes := spansNamed(sr, "eventwriter.flush")
	if len(flushes) != 1 {
		t.Fatalf("got %d flush spans, want 1", len(flushes))
	}

	links := persisted[0].Links()
	if len(links) != 1 {
		t.Fatalf("persist span has %d links, want 1", len(links))
	}
	if links[0].SpanContext.SpanID() != flushes[0].SpanContext().SpanID() {
		t.Errorf("persist span links to %s, want the flush span %s",
			links[0].SpanContext.SpanID(), flushes[0].SpanContext().SpanID())
	}
}

// A failed batch has to mark every contributing notification, not just the
// batch span -- otherwise the failure is invisible from the only trace anyone
// investigating a specific notification would look at.
func TestFlush_InsertFailureIsRecordedOnEveryItemSpan(t *testing.T) {
	sr := newRecordingTracer(t)
	w := testWriter(&fakeEventStore{insertErr: errors.New("postgres is down")})

	w.flush([]BatchItem[*hermenats.EventMessage]{
		{Ctx: ctxOnItsOwnTrace(t, "consume.a"), Msg: &hermenats.EventMessage{NotificationID: "n_a", Event: "inbox.sent"}},
		{Ctx: ctxOnItsOwnTrace(t, "consume.b"), Msg: &hermenats.EventMessage{NotificationID: "n_b", Event: "email.sent"}},
	})

	persisted := spansNamed(sr, "notification.event.persist")
	if len(persisted) != 2 {
		t.Fatalf("got %d persist spans, want 2 (they must be ended even on the error path)", len(persisted))
	}
	for _, s := range persisted {
		if s.Status().Code != codes.Error {
			t.Errorf("persist span for a failed batch has status %v, want Error", s.Status().Code)
		}
	}
}

// An event whose context carries no span must not mint a fresh root: that would
// recreate exactly the orphan traces this change removes.
func TestFlush_ItemWithoutATraceGetsNoSpan(t *testing.T) {
	sr := newRecordingTracer(t)
	w := testWriter(&fakeEventStore{})

	w.flush([]BatchItem[*hermenats.EventMessage]{
		{Ctx: context.Background(), Msg: &hermenats.EventMessage{NotificationID: "n_a", Event: "inbox.sent"}},
	})

	if n := len(spansNamed(sr, "notification.event.persist")); n != 0 {
		t.Errorf("got %d persist spans for an untraced item, want 0", n)
	}
	if n := len(spansNamed(sr, "eventwriter.flush")); n != 1 {
		t.Errorf("got %d flush spans, want 1", n)
	}
}

// The writer still does its actual job.
func TestFlush_WritesEventsAndStatusUpdates(t *testing.T) {
	newRecordingTracer(t)
	st := &fakeEventStore{}
	w := testWriter(st)

	w.flush([]BatchItem[*hermenats.EventMessage]{
		{Ctx: ctxOnItsOwnTrace(t, "c"), Msg: &hermenats.EventMessage{NotificationID: "n_a", Channel: "inbox", Event: "inbox.sent"}},
		{Ctx: ctxOnItsOwnTrace(t, "c"), Msg: &hermenats.EventMessage{NotificationID: "n_b", Channel: "inbox", Event: "routing.no_channels"}},
	})

	if len(st.inserted) != 2 {
		t.Errorf("inserted %d events, want 2", len(st.inserted))
	}
	// Only inbox.sent maps to a status; routing.no_channels does not.
	if len(st.updates) != 1 {
		t.Fatalf("got %d status updates, want 1", len(st.updates))
	}
	if st.updates[0].NewStatus != models.StatusDelivered {
		t.Errorf("status = %v, want delivered", st.updates[0].NewStatus)
	}
}
