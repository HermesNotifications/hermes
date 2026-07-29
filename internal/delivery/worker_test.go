// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

// mockProvider is a test implementation of Provider.
type mockProvider struct {
	name    string
	sendFn  func(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
	lastReq *DeliveryRequest
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	m.lastReq = &req
	if m.sendFn != nil {
		return m.sendFn(ctx, req)
	}
	return DeliveryResult{Success: true, ProviderName: m.name, ProviderID: "mock-id"}, nil
}

func newTestWorker(provider Provider, channel string) *Worker {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Worker{
		nats:     nil, // nil is safe; publishEvent guards against it
		provider: provider,
		channel:  channel,
		consumer: "test-consumer",
		logger:   logger,
	}
}

// attempt and lastAttempt build the DeliveryInfo the messaging layer passes in. The
// distinction is load-bearing: it is what decides whether a failure is reported once or
// once per retry.
func attempt(n uint64) messaging.DeliveryInfo {
	return messaging.DeliveryInfo{Attempt: n}
}

func lastAttempt(n uint64) messaging.DeliveryInfo {
	return messaging.DeliveryInfo{Attempt: n, LastAttempt: true}
}

// isPermanent mirrors what internal/messaging asks of a returned error when deciding
// between a nack-and-retry and a straight-to-DLQ termination.
func isPermanent(err error) bool {
	var pe messaging.PermanentError
	return errors.As(err, &pe) && pe.Permanent()
}

func marshalDelivery(t *testing.T, msg hermenats.DeliveryMessage) []byte {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal delivery message: %v", err)
	}
	return data
}

func TestWorker_HandleMessage_Success(t *testing.T) {
	provider := &mockProvider{name: "test-provider"}
	w := newTestWorker(provider, "email")

	actionURL := "https://example.com"
	actionLabel := "Open"
	msg := hermenats.DeliveryMessage{
		NotificationID: "notif-abc",
		OrganizationID: "org-1",
		UserID:         "user-1",
		Channel:        "email",
		Content: hermenats.MessageContent{
			Title:       "Hello",
			Body:        "World",
			ActionURL:   &actionURL,
			ActionLabel: &actionLabel,
		},
	}

	err := w.handleMessage(context.Background(), marshalDelivery(t, msg), attempt(1))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if provider.lastReq == nil {
		t.Fatal("expected provider.Send to be called")
	}
	if provider.lastReq.NotificationID != "notif-abc" {
		t.Errorf("expected NotificationID 'notif-abc', got %q", provider.lastReq.NotificationID)
	}
	if provider.lastReq.Title != "Hello" {
		t.Errorf("expected Title 'Hello', got %q", provider.lastReq.Title)
	}
	if provider.lastReq.ActionURL != actionURL {
		t.Errorf("expected ActionURL %q, got %q", actionURL, provider.lastReq.ActionURL)
	}
	if provider.lastReq.ActionLabel != actionLabel {
		t.Errorf("expected ActionLabel %q, got %q", actionLabel, provider.lastReq.ActionLabel)
	}
}

// Finding 9. Both assertions below previously required the OPPOSITE: that
// handleMessage return nil — an ack — on provider and unmarshal errors. They are
// inverted rather than removed, deliberately and visibly.
//
// Returning nil told the messaging layer the message succeeded, so it was acked and
// dropped. The retry, backoff and dead-letter machinery in internal/messaging was
// therefore unreachable from every delivery worker, and a transient SMTP or webhook
// blip permanently lost the notification. The old tests pinned that as correct, so the
// suite was defending the defect: any fix would have "broken" a passing test.

func TestWorker_HandleMessage_ReturnsProviderErrorSoItIsRetried(t *testing.T) {
	provider := &mockProvider{
		name: "failing-provider",
		sendFn: func(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
			return DeliveryResult{}, errors.New("downstream unavailable")
		},
	}
	w := newTestWorker(provider, "sms")

	msg := hermenats.DeliveryMessage{
		NotificationID: "notif-xyz",
		Channel:        "sms",
		Content:        hermenats.MessageContent{Title: "Alert", Body: "Something happened"},
	}

	err := w.handleMessage(context.Background(), marshalDelivery(t, msg), attempt(1))
	if err == nil {
		t.Fatal("expected an error so the message is nacked and retried; nil acks and drops it")
	}
	// A provider failure is assumed transient: the messaging layer retries it and
	// dead-letters at maxDeliveries. Marking it permanent would drop it on attempt one.
	if isPermanent(err) {
		t.Error("a provider error must not be permanent, or it is never retried")
	}
}

// A message that cannot be unmarshalled will never unmarshal, so retrying it ten times
// wastes nine attempts and delays the dead-letter. Permanent sends it straight to the DLQ
// with a reason of terminated rather than max-deliveries, which is the accurate diagnosis.
func TestWorker_HandleMessage_TreatsUnmarshalFailureAsPermanent(t *testing.T) {
	provider := &mockProvider{name: "test-provider"}
	w := newTestWorker(provider, "email")

	err := w.handleMessage(context.Background(), []byte("not valid json"), attempt(1))
	if err == nil {
		t.Fatal("expected an error; returning nil acks an unparseable message and loses it silently")
	}
	if !isPermanent(err) {
		t.Error("an unparseable message must be permanent — retrying it cannot help")
	}
	if provider.lastReq != nil {
		t.Error("provider.Send should not be called on unmarshal error")
	}
}

// The failed event fires once, on the final attempt — not on every retry. Publishing it
// each time would put up to maxDeliveries "failed" events on the stream for a single
// notification, which the status rollup and any alert counting them would both misread.
func TestWorker_HandleMessage_PublishesFailedEventOnlyOnTheLastAttempt(t *testing.T) {
	provider := &mockProvider{
		name: "failing-provider",
		sendFn: func(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
			return DeliveryResult{}, errors.New("downstream unavailable")
		},
	}

	msg := hermenats.DeliveryMessage{
		NotificationID: "notif-xyz",
		Channel:        "sms",
		Content:        hermenats.MessageContent{Title: "Alert", Body: "Something happened"},
	}

	cases := []struct {
		name     string
		info     messaging.DeliveryInfo
		wantHeld bool
	}{
		{"holds the event on an intermediate attempt", attempt(1), true},
		{"publishes the event on the last attempt", lastAttempt(10), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorker(provider, "sms")
			var published []string
			w.publishEventFn = func(_ context.Context, _, event, _ string, _ map[string]any) {
				published = append(published, event)
			}

			if err := w.handleMessage(context.Background(), marshalDelivery(t, msg), tc.info); err == nil {
				t.Fatal("expected an error")
			}

			held := len(published) == 0
			if held != tc.wantHeld {
				t.Errorf("published %v; wanted the failed event held = %v", published, tc.wantHeld)
			}
		})
	}
}

func TestWorker_HandleMessage_NoActionFields(t *testing.T) {
	provider := &mockProvider{name: "test-provider"}
	w := newTestWorker(provider, "inbox")

	msg := hermenats.DeliveryMessage{
		NotificationID: "notif-1",
		Channel:        "inbox",
		Content:        hermenats.MessageContent{Title: "Hi", Body: "There"},
	}

	err := w.handleMessage(context.Background(), marshalDelivery(t, msg), attempt(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider.lastReq.ActionURL != "" {
		t.Errorf("expected empty ActionURL, got %q", provider.lastReq.ActionURL)
	}
	if provider.lastReq.ActionLabel != "" {
		t.Errorf("expected empty ActionLabel, got %q", provider.lastReq.ActionLabel)
	}
}
