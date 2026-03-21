package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleCreateGroup(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"alerts","name":"Alerts","default_channels":["email"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var g models.NotificationGroup
	if err := json.NewDecoder(rec.Body).Decode(&g); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if g.Slug != "alerts" {
		t.Errorf("expected slug 'alerts', got %q", g.Slug)
	}
	if g.Name != "Alerts" {
		t.Errorf("expected name 'Alerts', got %q", g.Name)
	}
	if g.ID == "" {
		t.Error("expected non-empty ID in response")
	}
}

func TestHandleCreateGroup_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	// Missing slug — only name provided
	body := `{"name":"Alerts"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected non-empty error field in response")
	}
}

func TestHandleListGroups(t *testing.T) {
	srv := newTestServer(t)

	// First, create a group
	createBody := `{"slug":"billing","name":"Billing"}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewBufferString(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	// Now list groups
	listReq := httptest.NewRequest(http.MethodGet, "/v1/groups", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var groups []models.NotificationGroup
	if err := json.NewDecoder(listRec.Body).Decode(&groups); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Slug != "billing" {
		t.Errorf("expected slug 'billing', got %q", groups[0].Slug)
	}
}
