// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package centrifugo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Publish_Success(t *testing.T) {
	var capturedChannel string
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/publish" {
			t.Errorf("expected path /api/publish, got %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		capturedChannel, _ = payload["channel"].(string)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.Publish(context.Background(), "user#123", map[string]string{"title": "Hello"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if capturedChannel != "user#123" {
		t.Errorf("expected channel 'user#123', got %q", capturedChannel)
	}
	if capturedAuth != "apikey test-api-key" {
		t.Errorf("expected Authorization 'apikey test-api-key', got %q", capturedAuth)
	}
}

func TestClient_Publish_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.Publish(context.Background(), "user#123", map[string]string{"title": "Hello"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// The failure mode that had no coverage, and the one Centrifugo actually uses.
//
// Its server API answers 200 for *logical* failures and puts the reason in the body; a non-200
// only happens for transport-level problems, and only when the caller opts in with
// X-Centrifugo-Error-Mode. Checking the status alone reported a refused publish as delivered:
// the worker acked the message, the notification was marked delivered, and nobody received it.
// The two tests above pass identically with that defect present, which is how it survived.
func TestClient_Publish_LogicalErrorInA200Body(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown channel",
			body: `{"error":{"code":102,"message":"unknown channel"}}`,
			want: "unknown channel",
		},
		{
			name: "permission denied",
			body: `{"error":{"code":103,"message":"permission denied"}}`,
			want: "permission denied",
		},
		{
			name: "error alongside a result object",
			body: `{"error":{"code":107,"message":"bad request"},"result":{}}`,
			want: "bad request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-api-key")
			err := client.Publish(context.Background(), "user#123", map[string]string{"title": "Hello"})
			if err == nil {
				t.Fatal("expected an error for a 200 carrying an error envelope, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should name the reason %q, got: %v", tc.want, err)
			}
			// The channel belongs in the message: one worker publishes for every user, so
			// "permission denied" alone does not say whose notification was dropped.
			if !strings.Contains(err.Error(), "user#123") {
				t.Errorf("error should name the channel, got: %v", err)
			}
		})
	}
}

func TestClient_Publish_SuccessEnvelopeIsNotAnError(t *testing.T) {
	// What Centrifugo actually returns on success: an envelope with no error and an empty
	// result. A check that treated any body as suspicious would break every publish.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	if err := client.Publish(context.Background(), "user#123", map[string]string{"t": "x"}); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestClient_Publish_EmptyBodyIsTreatedAsSuccess(t *testing.T) {
	// Real Centrifugo always sends the envelope, but a proxy that strips it must not
	// manufacture delivery failures — and every retry costs a duplicate publish attempt.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	if err := client.Publish(context.Background(), "user#123", map[string]string{"t": "x"}); err != nil {
		t.Fatalf("expected success for an empty body, got: %v", err)
	}
}

func TestClient_Publish_UnparseableBodyIsAnError(t *testing.T) {
	// A 200 whose body is not the envelope means we are not talking to Centrifugo — a
	// misrouted ingress serving an HTML page, say. Reporting success there is the same
	// silent-drop bug wearing a different hat.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>404 Not Found</body></html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.Publish(context.Background(), "user#123", map[string]string{"t": "x"})
	if err == nil {
		t.Fatal("expected an error for a non-envelope 200 body, got nil")
	}
}
