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

// sendBody posts a body and returns the recorder, for the metadata cases below.
func sendBody(t *testing.T, pub *mockPublisher, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := newTestServerWithPublisher(t, pub)
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestSendHandler_MetadataReachesTheBus(t *testing.T) {
	pub := &mockPublisher{}
	w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},`+
		`"content":{"title":"Hi","body":"Hello"},"channels":["inbox"],`+
		`"metadata":{"level":"warning","toast":true,"invoiceId":"1041","nested":{"a":[1,2]}}}`)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}

	var msg struct {
		ClientMetadata map[string]any `json:"client_metadata"`
	}
	if err := json.Unmarshal(pub.published[0].Data, &msg); err != nil {
		t.Fatalf("unmarshal published message: %v", err)
	}

	if msg.ClientMetadata["level"] != "warning" {
		t.Errorf("level did not survive: %#v", msg.ClientMetadata["level"])
	}
	if msg.ClientMetadata["toast"] != true {
		t.Errorf("toast did not survive: %#v", msg.ClientMetadata["toast"])
	}
	// The keys Hermes does not interpret are the whole point of a passthrough.
	if msg.ClientMetadata["invoiceId"] != "1041" {
		t.Errorf("opaque key did not survive: %#v", msg.ClientMetadata["invoiceId"])
	}
	if _, ok := msg.ClientMetadata["nested"].(map[string]any); !ok {
		t.Errorf("nested object did not survive: %#v", msg.ClientMetadata["nested"])
	}
}

func TestSendHandler_NoMetadataLeavesTheKeyOffTheWire(t *testing.T) {
	// Asserted on the raw bytes rather than the decoded struct: `omitempty` on the map is what
	// keeps an existing consumer's frame byte-identical, and a struct comparison cannot see it.
	pub := &mockPublisher{}
	w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},"template":"welcome"}`)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(string(pub.published[0].Data), "client_metadata") {
		t.Errorf("client_metadata present on a send that supplied none: %s", pub.published[0].Data)
	}
}

func TestSendHandler_RejectsUnrecognisedLevel(t *testing.T) {
	// The enum lives in models.NotificationMetadata's huma schema, so this is rejected before
	// the handler body runs. Rejecting rather than coercing is deliberate: `level` is optional,
	// so only a caller who typed something wrong is affected, and they are the one who can fix
	// it. Coercing to "info" would invent intent, silently and durably.
	pub := &mockPublisher{}
	w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},`+
		`"content":{"title":"Hi","body":"Hello"},"channels":["inbox"],`+
		`"metadata":{"level":"critical"}}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for an unrecognised level, got %d: %s", w.Code, w.Body.String())
	}
	if len(pub.published) != 0 {
		t.Error("a rejected request still reached the bus")
	}
}

func TestSendHandler_RejectsNonBooleanToast(t *testing.T) {
	pub := &mockPublisher{}
	w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},`+
		`"content":{"title":"Hi","body":"Hello"},"channels":["inbox"],`+
		`"metadata":{"toast":"yes"}}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a non-boolean toast, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendHandler_AcceptsEveryDeclaredLevel(t *testing.T) {
	for _, level := range []string{"info", "success", "warning", "error"} {
		t.Run(level, func(t *testing.T) {
			pub := &mockPublisher{}
			w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},`+
				`"content":{"title":"Hi","body":"Hello"},"channels":["inbox"],`+
				`"metadata":{"level":"`+level+`"}}`)

			if w.Code != http.StatusAccepted {
				t.Fatalf("expected 202 for level %q, got %d: %s", level, w.Code, w.Body.String())
			}
		})
	}
}

func TestSendHandler_RejectsOversizeMetadata(t *testing.T) {
	// The blob is stored per notification and replayed to every connected client, so it is
	// bounded at the only point that can tell the caller about it.
	pub := &mockPublisher{}
	w := sendBody(t, pub, `{"to":{"organization_id":"t1","user_id":"u1"},`+
		`"content":{"title":"Hi","body":"Hello"},"channels":["inbox"],`+
		`"metadata":{"blob":"`+strings.Repeat("x", 5000)+`"}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize metadata, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "4096") {
		t.Errorf("the error does not tell the caller the limit: %s", w.Body.String())
	}
	if len(pub.published) != 0 {
		t.Error("an oversize request still reached the bus")
	}
}
