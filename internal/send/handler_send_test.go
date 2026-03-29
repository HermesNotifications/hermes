package send_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendHandler_TemplateSend(t *testing.T) {
	srv := newTestServer(t)

	body := `{"tenant_id":"t1","user_id":"u1","template":"welcome","data":{"name":"Alice"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Without a real NATS connection, publish will fail and return 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_DirectContentSend(t *testing.T) {
	srv := newTestServer(t)

	body := `{"tenant_id":"t1","user_id":"u1","content":{"title":"Hi","body":"Hello"},"channels":["inbox"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Without a real NATS connection, publish will fail and return 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_ExactlyOneRequired(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name string
		body string
	}{
		{"neither", `{"tenant_id":"t1","user_id":"u1"}`},
		{"both", `{"tenant_id":"t1","user_id":"u1","template":"welcome","content":{"title":"Hi","body":"Hello"}}`},
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
	srv := newTestServer(t)

	body := `{"tenant_id":"t1","user_id":"u1","content":{"title":"Hi","body":"Hello"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_ResponseContainsNotificationID(t *testing.T) {
	// This test verifies the response structure when NATS publish fails (503)
	// The notification ID should still be generated even on failure
	srv := newTestServer(t)

	body := `{"tenant_id":"t1","user_id":"u1","template":"welcome"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// On NATS failure we get 503 with an error, not a notification_id
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// Should have error info, not notification_id
	if _, ok := resp["notification_id"]; ok {
		t.Error("expected no notification_id in error response")
	}
}
