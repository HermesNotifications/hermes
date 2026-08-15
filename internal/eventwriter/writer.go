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
	// span connects to every input trace in Tempo's service graph.
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
		observability.RecordError(span, err)
		w.logger.ErrorContext(ctx, "batch insert events", "error", err, "count", len(items))
		return
	}

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
			w.logger.ErrorContext(ctx, "batch update statuses", "error", err, "count", len(updates))
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
