// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleListAPIKeys(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateAPIKey(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Test Key","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		RawKey      string   `json:"raw_key"`
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Name != "Test Key" {
		t.Fatalf("expected name 'Test Key', got %s", resp.Name)
	}
	if resp.RawKey == "" {
		t.Fatal("expected raw_key to be set")
	}
	if len(resp.Permissions) != 1 || resp.Permissions[0] != "notifications:send" {
		t.Fatalf("expected permissions [notifications:send], got %v", resp.Permissions)
	}
}

func TestHandleCreateAPIKey_InvalidPermission(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Bad Key","permissions":["foo:bar"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateAPIKey_DefaultPermissions(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Default Key"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Permissions) != 3 {
		t.Fatalf("expected 3 default permissions, got %d: %v", len(resp.Permissions), resp.Permissions)
	}
}

func TestHandleDeleteAPIKey(t *testing.T) {
	srv := newTestServer(t)

	// Create a key first
	body := `{"name":"To Delete","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var createResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(rec.Body).Decode(&createResp)
	keyID := createResp.ID

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/v1/apikeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteAPIKey_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/apikeys/key_nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Per-credential rate limits (ADR 0016) ---
//
// The columns and the enforcement shipped before any of this did, which meant a limit could
// only be set with direct SQL and was invisible through the API. These pin the management
// surface that closed that.

type apiKeyBody struct {
	ID                 string `json:"id"`
	RawKey             string `json:"raw_key"`
	RateLimitPerSecond *int   `json:"rate_limit_per_second"`
	RateLimitBurst     *int   `json:"rate_limit_burst"`
}

func createKey(t *testing.T, srv interface{ Handler() http.Handler }, body string) apiKeyBody {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out apiKeyBody
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return out
}

func setRateLimit(t *testing.T, srv interface{ Handler() http.Handler }, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/apikeys/"+id+"/rate-limit", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandleCreateAPIKey_WithRateLimit(t *testing.T) {
	srv := newTestServer(t)

	key := createKey(t, srv, `{"name":"Premium","rate_limit_per_second":500,"rate_limit_burst":1000}`)

	if key.RateLimitPerSecond == nil || *key.RateLimitPerSecond != 500 {
		t.Errorf("expected per-second 500, got %v", key.RateLimitPerSecond)
	}
	if key.RateLimitBurst == nil || *key.RateLimitBurst != 1000 {
		t.Errorf("expected burst 1000, got %v", key.RateLimitBurst)
	}
}

// Omitting the limits is how a caller asks for the service default, so they must come back
// absent rather than as zero — zero would be indistinguishable from a deliberate throttle.
func TestHandleCreateAPIKey_WithoutRateLimitLeavesItUnset(t *testing.T) {
	srv := newTestServer(t)

	key := createKey(t, srv, `{"name":"Standard"}`)

	if key.RateLimitPerSecond != nil || key.RateLimitBurst != nil {
		t.Errorf("expected no override, got %v / %v", key.RateLimitPerSecond, key.RateLimitBurst)
	}
}

func TestHandleSetAPIKeyRateLimit(t *testing.T) {
	srv := newTestServer(t)
	key := createKey(t, srv, `{"name":"Standard"}`)

	rec := setRateLimit(t, srv, key.ID, `{"per_second":50,"burst":100}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out apiKeyBody
	json.NewDecoder(rec.Body).Decode(&out)
	if out.RateLimitPerSecond == nil || *out.RateLimitPerSecond != 50 {
		t.Errorf("expected per-second 50, got %v", out.RateLimitPerSecond)
	}
	if out.RateLimitBurst == nil || *out.RateLimitBurst != 100 {
		t.Errorf("expected burst 100, got %v", out.RateLimitBurst)
	}
}

// The endpoint is a PUT on the whole limit, so an empty body is how an override is removed.
func TestHandleSetAPIKeyRateLimit_EmptyBodyClearsTheOverride(t *testing.T) {
	srv := newTestServer(t)
	key := createKey(t, srv, `{"name":"Premium","rate_limit_per_second":500,"rate_limit_burst":1000}`)

	rec := setRateLimit(t, srv, key.ID, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out apiKeyBody
	json.NewDecoder(rec.Body).Decode(&out)
	if out.RateLimitPerSecond != nil || out.RateLimitBurst != nil {
		t.Errorf("expected the override cleared, got %v / %v", out.RateLimitPerSecond, out.RateLimitBurst)
	}
}

// Replacement, not merge: setting only one field must clear the other, or the documented
// PUT semantics are a lie.
func TestHandleSetAPIKeyRateLimit_ReplacesRatherThanMerges(t *testing.T) {
	srv := newTestServer(t)
	key := createKey(t, srv, `{"name":"Premium","rate_limit_per_second":500,"rate_limit_burst":1000}`)

	rec := setRateLimit(t, srv, key.ID, `{"per_second":50}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out apiKeyBody
	json.NewDecoder(rec.Body).Decode(&out)
	if out.RateLimitPerSecond == nil || *out.RateLimitPerSecond != 50 {
		t.Errorf("expected per-second 50, got %v", out.RateLimitPerSecond)
	}
	if out.RateLimitBurst != nil {
		t.Errorf("expected burst cleared, got %v", out.RateLimitBurst)
	}
}

// Zero would reach the database and trip its CHECK constraint, turning a typo into a 500.
func TestHandleSetAPIKeyRateLimit_RejectsZero(t *testing.T) {
	srv := newTestServer(t)
	key := createKey(t, srv, `{"name":"Standard"}`)

	rec := setRateLimit(t, srv, key.ID, `{"per_second":0}`)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected a validation error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetAPIKeyRateLimit_NotFound(t *testing.T) {
	srv := newTestServer(t)

	rec := setRateLimit(t, srv, "key_nonexistent", `{"per_second":50}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A limit nobody can see is a limit nobody can audit.
func TestHandleListAPIKeys_ExposesTheRateLimit(t *testing.T) {
	srv := newTestServer(t)
	createKey(t, srv, `{"name":"Premium","rate_limit_per_second":500}`)

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var keys []apiKeyBody
	json.NewDecoder(rec.Body).Decode(&keys)

	var found bool
	for _, k := range keys {
		if k.RateLimitPerSecond != nil && *k.RateLimitPerSecond == 500 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the listing to carry the rate limit, got %+v", keys)
	}
}
