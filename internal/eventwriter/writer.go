// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package eventwriter

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/models"
	hermenats "github.com/hermesnotifications/hermes/internal/nats"
	"github.com/hermesnotifications/hermes/internal/observability"
	"github.com/hermesnotifications/hermes/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Writer struct {
	nats   *messaging.Client
	store  store.EventRepository
	logger *slog.Logger
	batch  *Batch[*hermenats.EventMessage]
}

func New(nats *messaging.Client, st store.EventRepository, logger *slog.Logger) *Writer {
	w := &Writer{
		nats:   nats,
		store:  st,
		logger: logger,
	}
	w.batch = NewBatch[*hermenats.EventMessage](100, 500*time.Millisecond, w.flush)
	return w
}

func (w *Writer) Start(_ context.Context) error {
	// One worker feeds the in-memory batcher, which flushes on size/time; a deep
	// prefetch keeps the batch fed (firehose) rather than throttling per message.
	return w.nats.Subscribe(messaging.SubscribeConfig{
		Subject:       "notification.events",
		Consumer:      "event-writer",
		MaxAckPending: 1000,
		Workers:       1,
		Prefetch:      256,
		// Short, because this handler only appends to an in-memory batch — the database write
		// happens on the flush timer, not here. Anything approaching ten seconds means the batch
		// itself is wedged, and redelivering is the right answer.
		HandlerTimeout: 10 * time.Second,
		AckWait:        30 * time.Second,
	}, func(ctx context.Context, data []byte, _ messaging.DeliveryInfo) error {
		msg, err := hermenats.UnmarshalEvent(data)
		if err != nil {
			w.logger.Error("unmarshal event", "error", err)
			return nil // don't retry bad messages
		}
		w.batch.Add(ctx, msg)
		return nil
	})
}

func (w *Writer) Stop() {
	w.batch.Flush()
}

func (w *Writer) flush(items []BatchItem[*hermenats.EventMessage]) {
	// Collect span links back to each originating trace so the batch flush
	// span connects to every input trace.
	// (Replaces the DSM-context merge from the dd-trace-go version —
	// DSM is DD-proprietary; see docs/observability/adr/004-accepting-dsm-loss.md.)
	var links []trace.Link
	for _, item := range items {
		if sc := trace.SpanContextFromContext(item.Ctx); sc.IsValid() {
			links = append(links, trace.Link{SpanContext: sc})
		}
	}

	tracer := otel.Tracer("github.com/hermesnotifications/hermes/internal/eventwriter")
	ctx, span := tracer.Start(context.Background(), "eventwriter.flush",
		trace.WithLinks(links...),
		trace.WithAttributes(
			attribute.Int("batch.size", len(items)),
			attribute.String("messaging.destination", "notification.events"),
		),
	)
	defer span.End()

	started := time.Now()
	batchSize.Record(ctx, int64(len(items)))

	// The links above are the correct OTel shape for a fan-in, but they are only
	// half the picture, and in practice the less useful half: a flush is a new
	// root trace, so every status write landed in a trace of its own. Asking "did
	// this notification's status actually get written?" meant leaving the
	// notification's trace and searching by ID. ADR 0004 assumed a backend that
	// walks links to stitch that back together; the one we deploy does not.
	//
	// So each item also gets a short span on *its own* trace, linked back to the
	// batch. The flush keeps the batch-level timing; each notification gains the
	// one fact it was missing.
	//
	// Cost: worker-events is the highest-call service in the fleet, and this is
	// roughly one extra span per event. Only items carrying a live trace context
	// get one -- starting from a context without one would mint a fresh root and
	// recreate the very orphans this removes.
	itemSpans := make([]trace.Span, 0, len(items))
	for _, item := range items {
		if !trace.SpanContextFromContext(item.Ctx).IsValid() {
			continue
		}
		_, itemSpan := tracer.Start(item.Ctx, "notification.event.persist",
			trace.WithLinks(trace.Link{SpanContext: span.SpanContext()}),
			trace.WithAttributes(
				attribute.String("event", item.Msg.Event),
				attribute.String("notification.id", item.Msg.NotificationID),
				attribute.String("channel", item.Msg.Channel),
			),
		)
		itemSpans = append(itemSpans, itemSpan)
	}
	// The batch succeeds or fails as a unit, so every item carries the same outcome.
	var flushErr error

	// Deferred so the failure paths below, which return early, are timed too — a flush
	// that fails slowly is the interesting one, and timing only the success path would
	// leave the histogram looking healthiest exactly when the database is not. The item
	// spans are ended here for the same reason: an early return must not strand them.
	defer func() {
		flushDuration.Record(ctx, time.Since(started).Seconds())
		for _, itemSpan := range itemSpans {
			observability.RecordError(itemSpan, flushErr)
			itemSpan.End()
		}
	}()

	// Convert to models for DB insert.
	dbEvents := make([]models.NotificationEvent, len(items))
	for i, item := range items {
		e := item.Msg
		var metadata []byte
		if e.Metadata != nil {
			metadata, _ = json.Marshal(e.Metadata)
		}
		dbEvents[i] = models.NotificationEvent{
			NotificationID: e.NotificationID,
			Channel:        e.Channel,
			Event:          e.Event,
			Severity:       e.Severity,
			Metadata:       metadata,
		}
	}

	// Batch insert events.
	if err := w.store.InsertEvents(ctx, dbEvents); err != nil {
		flushErr = err
		observability.RecordError(span, err)
		w.logger.ErrorContext(ctx, "batch insert events", "error", err, "count", len(items))
		eventsDropped.Add(ctx, int64(len(items)), metric.WithAttributes(
			attribute.String("stage", "insert"),
		))
		return
	}
	eventsWritten.Add(ctx, int64(len(items)))

	// Collect status updates from events.
	var updates []store.StatusUpdate
	now := time.Now()
	for _, item := range items {
		e := item.Msg
		status := eventToStatus(e.Event)
		if status != "" {
			updates = append(updates, store.StatusUpdate{
				NotificationID: e.NotificationID,
				NewStatus:      status,
				EventTime:      now,
			})
		}
	}

	// Batch update notification statuses.
	if len(updates) > 0 {
		if err := w.store.BatchUpdateNotificationStatuses(ctx, updates); err != nil {
			// Not assigned to flushErr: the events themselves are durable at this
			// point, and the status rollup only ever advances, so the next event
			// for this notification recovers it. Recorded, not failed.
			observability.RecordError(span, err)
			w.logger.ErrorContext(ctx, "batch update statuses", "error", err, "count", len(updates))
			eventsDropped.Add(ctx, int64(len(updates)), metric.WithAttributes(
				attribute.String("stage", "status"),
			))
		}
	}

	// Debug: the batcher flushes on 100 items or 500ms, so this fired up to twice a
	// second per replica in steady state — roughly 170k records a day to report that
	// a batch insert did not fail. The eventwriter.flush span above already carries
	// batch.size, and the errors above are still Error.
	w.logger.DebugContext(ctx, "flushed events", "count", len(items))
}

// eventToStatus maps event names to notification statuses.
func eventToStatus(event string) models.NotificationStatus {
	switch event {
	case "notification.sent", "email.routed", "sms.routed", "inbox.routed":
		return models.StatusSent
	case "email.sent", "sms.sent", "inbox.sent":
		return models.StatusDelivered
	default:
		return "" // no status update
	}
}
