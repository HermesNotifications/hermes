package eventwriter

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
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

func (w *Writer) Start(ctx context.Context) error {
	return w.nats.Subscribe("notification.events", "event-writer", func(data []byte) error {
		msg, err := hermenats.UnmarshalEvent(data)
		if err != nil {
			w.logger.Error("unmarshal event", "error", err)
			return nil // don't retry bad messages
		}
		w.batch.Add(msg)
		return nil
	})
}

func (w *Writer) Stop() {
	w.batch.Flush()
}

func (w *Writer) flush(events []*hermenats.EventMessage) {
	ctx := context.Background()

	// Convert to models for DB insert
	dbEvents := make([]models.NotificationEvent, len(events))
	for i, e := range events {
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

	// Batch insert events
	if err := w.store.InsertEvents(ctx, dbEvents); err != nil {
		w.logger.Error("batch insert events", "error", err, "count", len(events))
		return
	}

	// Update notification statuses based on events
	for _, e := range events {
		status := eventToStatus(e.Event)
		if status != "" {
			if err := w.store.UpdateNotificationStatus(ctx, e.NotificationID, status, time.Now()); err != nil {
				w.logger.Error("update status", "error", err, "notification_id", e.NotificationID)
			}
		}
	}

	w.logger.Info("flushed events", "count", len(events))
}

// eventToStatus maps event names to notification statuses.
func eventToStatus(event string) models.NotificationStatus {
	switch event {
	case "notification.sent", "email.routed", "sms.routed", "inbox.routed":
		return models.StatusSent
	case "email.sent", "sms.sent", "inbox.delivered":
		return models.StatusDelivered
	default:
		return "" // no status update
	}
}
