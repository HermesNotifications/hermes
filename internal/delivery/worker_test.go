package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

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
		TenantID:       "tenant-1",
		UserID:         "user-1",
		Channel:        "email",
		Content: hermenats.MessageContent{
			Title:       "Hello",
			Body:        "World",
			ActionURL:   &actionURL,
			ActionLabel: &actionLabel,
		},
	}

	err := w.handleMessage(context.Background(), marshalDelivery(t, msg))
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

func TestWorker_HandleMessage_ProviderError(t *testing.T) {
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

	// handleMessage returns nil (ack the message) even on provider error
	err := w.handleMessage(context.Background(), marshalDelivery(t, msg))
	if err != nil {
		t.Fatalf("handleMessage should return nil on provider error, got: %v", err)
	}
}

func TestWorker_HandleMessage_InvalidJSON(t *testing.T) {
	provider := &mockProvider{name: "test-provider"}
	w := newTestWorker(provider, "email")

	err := w.handleMessage(context.Background(), []byte("not valid json"))
	if err != nil {
		t.Fatalf("handleMessage should return nil on unmarshal error, got: %v", err)
	}
	if provider.lastReq != nil {
		t.Error("provider.Send should not be called on unmarshal error")
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

	err := w.handleMessage(context.Background(), marshalDelivery(t, msg))
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
