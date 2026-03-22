package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCreateType(t *testing.T) {
	srv := newTestServer(t)

	// First create a group (types need group_id)
	groupBody := `{"slug":"billing","name":"Billing","default_channels":["email"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(groupBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d", rec.Code)
	}
	var group map[string]any
	json.NewDecoder(rec.Body).Decode(&group)
	groupID := group["id"].(string)

	// Create type
	body := fmt.Sprintf(`{"group_id":"%s","slug":"invoice.paid","name":"Invoice Paid"}`, groupID)
	req = httptest.NewRequest("POST", "/v1/types", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateType_MissingFields(t *testing.T) {
	srv := newTestServer(t)
	body := `{"slug":"invoice.paid"}`
	req := httptest.NewRequest("POST", "/v1/types", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestHandleDeleteType(t *testing.T) {
	srv := newTestServer(t)

	// Create group + type
	groupBody := `{"slug":"billing","name":"Billing","default_channels":["email"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(groupBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var group map[string]any
	json.NewDecoder(rec.Body).Decode(&group)
	groupID := group["id"].(string)

	body := fmt.Sprintf(`{"group_id":"%s","slug":"invoice.paid","name":"Invoice Paid"}`, groupID)
	req = httptest.NewRequest("POST", "/v1/types", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var typ map[string]any
	json.NewDecoder(rec.Body).Decode(&typ)
	typeID := typ["id"].(string)

	// Delete
	req = httptest.NewRequest("DELETE", "/v1/types/"+typeID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
