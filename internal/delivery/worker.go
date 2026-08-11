// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

// permanentError marks a failure that retrying cannot fix. It implements
// messaging.PermanentError, so internal/messaging terminates the message straight to the
// DLQ instead of nacking it through nine pointless redeliveries.
type permanentError struct{ err error }

func (e *permanentError) Error() string   { return e.err.Error() }
func (e *permanentError) Unwrap() error   { return e.err }
func (e *permanentError) Permanent() bool { return true }

func permanent(err error) error { return &permanentError{err: err} }

type Worker struct {
	nats     *messaging.Client
	provider Provider
	channel  string
	consumer string
	logger   *slog.Logger

	// publishEventFn allows tests to observe emitted events without a NATS connection.
	// Nil in production, where publishEvent is used directly.
	publishEventFn func(ctx context.Context, notificationID, event, severity string, metadata map[string]any)
}

func NewWorker(nats *messaging.Client, provider Provider, channel, consumer string, logger *slog.Logger) *Worker {
	return &Worker{nats: nats, provider: provider, channel: channel, consumer: consumer, logger: logger}
}

func (w *Worker) Start(_ context.Context) error {
	return w.nats.Subscribe(messaging.SubscribeConfig{
		Subject:       "delivery." + w.channel,
		Consumer:      w.consumer,
		MaxAckPending: 256,
		Workers:       4,
		// The slowest handlers in the system: an SMTP conversation, a customer's webhook, a
		// Centrifugo publish. AckWait must clear the handler deadline or JetStream redelivers a
		// message mid-send and the recipient gets the notification twice.
		HandlerTimeout: 30 * time.Second,
		AckWait:        60 * time.Second,
	}, func(ctx context.Context, data []byte, info messaging.DeliveryInfo) error {
		return w.handleMessage(ctx, data, info)
	})
}

// handleMessage delivers one notification.
//
// Finding 9. This previously returned nil on every failure path, which the messaging
// layer reads as success — so the message was acked and dropped, and the retry, backoff
// and dead-letter machinery in internal/messaging was unreachable from all three delivery
// workers. A transient SMTP or webhook blip permanently lost the notification.
//
// Now: an unparseable message is permanent (retrying cannot help), and a provider failure
// is transient (returned so the message is nacked, retried with backoff, and dead-lettered
// once maxDeliveries is exhausted).
func (w *Worker) handleMessage(ctx context.Context, data []byte, info messaging.DeliveryInfo) error {
	msg, err := hermenats.UnmarshalDelivery(data)
	if err != nil {
		w.logger.Error("unmarshal delivery", "error", err)
		return permanent(fmt.Errorf("unmarshal delivery: %w", err))
	}

	req := DeliveryRequest{
		NotificationID: msg.NotificationID,
		OrganizationID: msg.OrganizationID,
		UserID:         msg.UserID,
		Channel:        w.channel,
		Title:          msg.Content.Title,
		Body:           msg.Content.Body,
		EmailTo:        msg.Recipient["email"],
		PhoneTo:        msg.Recipient["phone"],
	}
	if msg.Content.ActionURL != nil {
		req.ActionURL = *msg.Content.ActionURL
	}
	if msg.Content.ActionLabel != nil {
		req.ActionLabel = *msg.Content.ActionLabel
	}

	result, err := w.provider.Send(ctx, req)
	if err != nil {
		w.logger.Error("delivery failed",
			"notification_id", msg.NotificationID, "channel", w.channel,
			"attempt", info.Attempt, "last_attempt", info.LastAttempt, "error", err)

		// Report the failure once, on the final attempt. Publishing on every attempt
		// would put up to maxDeliveries ".failed" events on the stream for one
		// notification — the status rollup would see repeats and any alert counting
		// them would read a single flaky delivery as a cluster of failures.
		if info.LastAttempt {
			w.emitEvent(ctx, msg.NotificationID, w.channel+".failed", "error", map[string]any{
				"error": err.Error(), "attempts": info.Attempt,
			})
		}

		// Returned, not swallowed: this is what nacks the message so it is retried with
		// backoff and dead-lettered once retries are exhausted. Deliberately NOT
		// permanent — a provider error is assumed transient because Provider.Send gives
		// no way to distinguish a 4xx rejection from a connection refused. Classifying
		// per-provider is worth doing and is a separate change.
		return fmt.Errorf("deliver via %s: %w", w.provider.Name(), err)
	}

	w.logger.Info("delivery succeeded", "notification_id", msg.NotificationID, "channel", w.channel)
	w.emitEvent(ctx, msg.NotificationID, w.channel+".sent", "info", map[string]any{
		"provider": result.ProviderName, "provider_id": result.ProviderID,
	})
	return nil
}

// emitEvent routes through publishEventFn when set, so tests can observe events without
// a NATS connection.
func (w *Worker) emitEvent(ctx context.Context, notificationID, event, severity string, metadata map[string]any) {
	if w.publishEventFn != nil {
		w.publishEventFn(ctx, notificationID, event, severity, metadata)
		return
	}
	w.publishEvent(ctx, notificationID, event, severity, metadata)
}

func (w *Worker) publishEvent(ctx context.Context, notificationID, event, severity string, metadata map[string]any) {
	if w.nats == nil {
		return
	}
	evt := &hermenats.EventMessage{
		NotificationID: notificationID,
		Channel:        w.channel,
		Event:          event,
		Severity:       severity,
		Metadata:       metadata,
	}
	evtBytes, _ := json.Marshal(evt)
	if err := w.nats.Publish(ctx, "notification.events", evtBytes); err != nil {
		w.logger.Error("publish event failed", "error", err)
	}
}
