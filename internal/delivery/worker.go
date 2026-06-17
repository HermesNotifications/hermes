// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package delivery

import (
	"context"
	"encoding/json"
	"log/slog"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

type Worker struct {
	nats     *messaging.Client
	provider Provider
	channel  string
	consumer string
	logger   *slog.Logger
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
	}, func(ctx context.Context, data []byte, _ messaging.DeliveryInfo) error {
		return w.handleMessage(ctx, data)
	})
}

func (w *Worker) handleMessage(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalDelivery(data)
	if err != nil {
		w.logger.Error("unmarshal delivery", "error", err)
		return nil
	}

	req := DeliveryRequest{
		NotificationID: msg.NotificationID,
		TenantID:       msg.TenantID,
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
		w.logger.Error("delivery failed", "notification_id", msg.NotificationID, "channel", w.channel, "error", err)
		w.publishEvent(ctx, msg.NotificationID, w.channel+".failed", "error", map[string]any{"error": err.Error()})
		return nil
	}

	w.logger.Info("delivery succeeded", "notification_id", msg.NotificationID, "channel", w.channel)
	w.publishEvent(ctx, msg.NotificationID, w.channel+".sent", "info", map[string]any{
		"provider": result.ProviderName, "provider_id": result.ProviderID,
	})
	return nil
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
