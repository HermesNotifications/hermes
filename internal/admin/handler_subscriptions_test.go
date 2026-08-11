// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleCreateSubscription(t *testing.T) {
	srv := newTestServer(t)

	// Create a category first
	catBody := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(catBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create category: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	// Create subscription
	body := `{"slug":"invoices","name":"Invoice Notifications","sort_order":0}`
	req = httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories/"+cat.ID+"/subscriptions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var sub models.Subscription
	json.NewDecoder(rec.Body).Decode(&sub)

	if sub.Slug != "invoices" {
		t.Errorf("expected slug 'invoices', got %q", sub.Slug)
	}
	if sub.CategoryID != cat.ID {
		t.Errorf("expected category_id %q, got %q", cat.ID, sub.CategoryID)
	}
	if sub.ID == "" {
		t.Error("expected ID to be set")
	}
}

func TestHandleCreateSubscription_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"invoices"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories/sct-1/subscriptions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListSubscriptions(t *testing.T) {
	store := &mockStore{
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "billing", Name: "Billing", DefaultState: "on"},
		},
		subscriptions: []models.Subscription{
			{ID: "sub-1", CategoryID: "sct-1", Slug: "invoices", Name: "Invoices"},
			{ID: "sub-2", CategoryID: "sct-1", Slug: "payments", Name: "Payments"},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/categories/sct-1/subscriptions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var subs []models.Subscription
	json.NewDecoder(rec.Body).Decode(&subs)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}
}

func TestHandleListSubscriptions_Empty(t *testing.T) {
	store := &mockStore{
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "billing", Name: "Billing", DefaultState: "on"},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/categories/sct-1/subscriptions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateSubscription(t *testing.T) {
	srv := newTestServer(t)

	// Create category
	catBody := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(catBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	// Create subscription
	body := `{"slug":"invoices","name":"Invoices","sort_order":0}`
	req = httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories/"+cat.ID+"/subscriptions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var sub models.Subscription
	json.NewDecoder(rec.Body).Decode(&sub)

	// Update
	updateBody := `{"name":"Invoice Alerts","sort_order":1}`
	req = httptest.NewRequest(http.MethodPut, "/v1/subscriptions/"+sub.ID, bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated models.Subscription
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated.Name != "Invoice Alerts" {
		t.Errorf("expected name 'Invoice Alerts', got %q", updated.Name)
	}
	if updated.SortOrder != 1 {
		t.Errorf("expected sort_order 1, got %d", updated.SortOrder)
	}
}

func TestHandleUpdateSubscription_NotFound(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Nope","sort_order":0}`
	req := httptest.NewRequest(http.MethodPut, "/v1/subscriptions/sub-nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteSubscription(t *testing.T) {
	srv := newTestServer(t)

	// Create category
	catBody := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(catBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	// Create subscription
	body := `{"slug":"invoices","name":"Invoices","sort_order":0}`
	req = httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories/"+cat.ID+"/subscriptions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var sub models.Subscription
	json.NewDecoder(rec.Body).Decode(&sub)

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/"+sub.ID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteSubscription_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/sub-nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
