package eventwriter

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/DataDog/dd-trace-go/v2/datastreams"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/store"
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
	return w.nats.Subscribe("notification.events", "event-writer", 1000, 1, func(ctx context.Context, data []byte, _ messaging.DeliveryInfo) error {
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
	// Merge DSM pathways (fan-in) from all batched messages.
	ctxs := make([]context.Context, len(items))
	for i, item := range items {
		ctxs[i] = item.Ctx
	}
	ctx := datastreams.MergeContexts(ctxs...)

	// Create flush span with links back to each originating trace.
	var links []tracer.SpanLink
	for _, item := range items {
		if sp, ok := tracer.SpanFromContext(item.Ctx); ok {
			links = append(links, tracer.SpanLink{
				TraceID:     sp.Context().TraceIDLower(),
				TraceIDHigh: sp.Context().TraceIDUpper(),
				SpanID:      sp.Context().SpanID(),
			})
		}
	}
	opts := []tracer.StartSpanOption{
		tracer.ResourceName("notification.events"),
	}
	if len(links) > 0 {
		opts = append(opts, tracer.WithSpanLinks(links))
	}
	span, ctx := tracer.StartSpanFromContext(ctx, "eventwriter.flush", opts...)
	defer span.Finish()

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
		span.SetTag("error", true)
		span.SetTag("error.message", err.Error())
		w.logger.Error("batch insert events", "error", err, "count", len(items))
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
			w.logger.Error("batch update statuses", "error", err, "count", len(updates))
		}
	}

	w.logger.Info("flushed events", "count", len(items))
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
