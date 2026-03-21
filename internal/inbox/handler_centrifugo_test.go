package inbox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestHandleCentrifugoToken(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox/centrifugo-token", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	tokenStr := resp["token"]
	if tokenStr == "" {
		t.Fatal("expected non-empty token")
	}

	// Verify the token
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return []byte("test-centrifugo-secret"), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("token should be valid")
	}

	sub, err := claims.GetSubject()
	if err != nil {
		t.Fatalf("get subject: %v", err)
	}
	if sub != testUserID {
		t.Fatalf("expected subject %q, got %q", testUserID, sub)
	}

	exp, err := claims.GetExpirationTime()
	if err != nil {
		t.Fatalf("get expiration: %v", err)
	}
	if exp == nil {
		t.Fatal("expected expiration to be set")
	}
}

func TestHandleCentrifugoToken_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox/centrifugo-token", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
