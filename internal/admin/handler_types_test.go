package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCreateTemplate(t *testing.T) {
	srv := newTestServer(t)

	// Create template
	body := `{"slug":"invoice.paid","name":"Invoice Paid","email_subject":"Invoice paid"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var tmpl map[string]any
	json.NewDecoder(rec.Body).Decode(&tmpl)
	if tmpl["slug"] != "invoice.paid" {
		t.Errorf("expected slug invoice.paid, got %v", tmpl["slug"])
	}
}

func TestHandleCreateTemplate_MissingFields(t *testing.T) {
	srv := newTestServer(t)
	body := `{"slug":"invoice.paid"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestHandleDeleteTemplate(t *testing.T) {
	srv := newTestServer(t)

	// Create template
	body := `{"slug":"invoice.paid","name":"Invoice Paid"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var tmpl map[string]any
	json.NewDecoder(rec.Body).Decode(&tmpl)
	templateID := tmpl["id"].(string)

	// Delete
	req = httptest.NewRequest("DELETE", "/v1/templates/"+templateID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
