// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package send_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bounding the work streams with DiscardNew means a publish can now fail because
// the pipeline is saturated, not only because NATS is unreachable. A 503 with no
// Retry-After leaves the client to invent a backoff, and the clients that invent
// one badly retry hardest exactly when the pipeline is already behind.
func TestSendHandler_PublishFailureCarriesRetryAfter(t *testing.T) {
	pub := &mockPublisher{err: fmt.Errorf("nats: maximum bytes exceeded")}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when the notification could not be published, got %d: %s",
			w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("503 carries no Retry-After; the client has nothing to back off against")
	}
}

func TestSendHandler_TemplateSend_Success(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome","data":{"name":"Alice"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NotificationID string `json:"notification_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.NotificationID == "" {
		t.Error("expected notification_id to be set")
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}
	if pub.published[0].Subject != "notification.send" {
		t.Errorf("expected subject 'notification.send', got %q", pub.published[0].Subject)
	}
}

func TestSendHandler_DirectSend_Success(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"content":{"title":"Hi","body":"Hello"},"channels":["inbox"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}
}

func TestSendHandler_NATSPublishError(t *testing.T) {
	pub := &mockPublisher{err: fmt.Errorf("connection refused")}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_NATSNil(t *testing.T) {
	// Without a publisher, the handler returns 503
	srv := newTestServer(t)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_ExactlyOneRequired(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	tests := []struct {
		name string
		body string
	}{
		{"neither", `{"to":{"organization_id":"t1","user_id":"u1"}}`},
		{"both", `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome","content":{"title":"Hi","body":"Hello"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSendHandler_DirectSendWithoutChannels(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"content":{"title":"Hi","body":"Hello"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_MissingRecipient(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Fatalf("expected 422 or 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_ResponseContainsNotificationID(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := resp["notification_id"]; !ok {
		t.Error("expected notification_id in response")
	}
}
