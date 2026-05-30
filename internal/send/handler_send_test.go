// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package send_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendHandler_TemplateSend_Success(t *testing.T) {
	pub := &mockPublisher{}
	srv := newTestServerWithPublisher(t, pub)

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"template":"welcome","data":{"name":"Alice"}}`
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

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"content":{"title":"Hi","body":"Hello"},"channels":["inbox"]}`
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

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"template":"welcome"}`
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

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"template":"welcome"}`
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
		{"neither", `{"to":{"tenant_id":"t1","user_id":"u1"}}`},
		{"both", `{"to":{"tenant_id":"t1","user_id":"u1"},"template":"welcome","content":{"title":"Hi","body":"Hello"}}`},
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

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"content":{"title":"Hi","body":"Hello"}}`
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

	body := `{"to":{"tenant_id":"t1","user_id":"u1"},"template":"welcome"}`
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
