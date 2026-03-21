package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func TestTypesList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	types := []NotificationType{
		{ID: "t1", GroupID: "g1", Slug: "welcome", Name: "Welcome", CreatedAt: now},
		{ID: "t2", GroupID: "g1", Slug: "alert", Name: "Alert", EmailSubject: ptr("Alert!"), CreatedAt: now},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/types" {
			t.Errorf("expected /v1/types, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Types.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 types, got %d", len(result))
	}
	if result[0].ID != "t1" || result[0].Slug != "welcome" {
		t.Errorf("unexpected first type: %+v", result[0])
	}
	if result[1].ID != "t2" || result[1].EmailSubject == nil || *result[1].EmailSubject != "Alert!" {
		t.Errorf("unexpected second type: %+v", result[1])
	}
}

func TestTypesCreate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	created := NotificationType{
		ID:           "t3",
		GroupID:      "g1",
		Slug:         "newsletter",
		Name:         "Newsletter",
		EmailSubject: ptr("Your weekly digest"),
		CreatedAt:    now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/types" {
			t.Errorf("expected /v1/types, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body CreateTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.GroupID != "g1" || body.Slug != "newsletter" || body.Name != "Newsletter" {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Types.Create(context.Background(), CreateTypeRequest{
		GroupID:      "g1",
		Slug:         "newsletter",
		Name:         "Newsletter",
		EmailSubject: ptr("Your weekly digest"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "t3" {
		t.Errorf("expected id t3, got %s", result.ID)
	}
	if result.Slug != "newsletter" {
		t.Errorf("expected slug newsletter, got %s", result.Slug)
	}
	if result.EmailSubject == nil || *result.EmailSubject != "Your weekly digest" {
		t.Errorf("unexpected email subject: %v", result.EmailSubject)
	}
}

func TestTypesUpdate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	updated := NotificationType{
		ID:           "t1",
		GroupID:      "g1",
		Slug:         "welcome",
		Name:         "Welcome Updated",
		EmailSubject: ptr("Welcome aboard!"),
		CreatedAt:    now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/types/t1" {
			t.Errorf("expected /v1/types/t1, got %s", r.URL.Path)
		}

		var body UpdateTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Name != "Welcome Updated" {
			t.Errorf("unexpected name in body: %s", body.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Types.Update(context.Background(), "t1", UpdateTypeRequest{
		Name:         "Welcome Updated",
		EmailSubject: ptr("Welcome aboard!"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "t1" {
		t.Errorf("expected id t1, got %s", result.ID)
	}
	if result.Name != "Welcome Updated" {
		t.Errorf("expected name 'Welcome Updated', got %s", result.Name)
	}
}

func TestTypesDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/types/t1" {
			t.Errorf("expected /v1/types/t1, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	err := c.Types.Delete(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
