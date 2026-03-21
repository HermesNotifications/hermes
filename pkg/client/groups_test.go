package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGroupsList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	groups := []Group{
		{ID: "g1", Slug: "alerts", Name: "Alerts", DefaultChannels: []string{"email"}, CreatedAt: now},
		{ID: "g2", Slug: "updates", Name: "Updates", DefaultChannels: []string{"sms", "inbox"}, CreatedAt: now},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/groups" {
			t.Errorf("expected /v1/groups, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(groups)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Groups.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
	if result[0].ID != "g1" || result[0].Slug != "alerts" {
		t.Errorf("unexpected first group: %+v", result[0])
	}
	if result[1].ID != "g2" || result[1].Slug != "updates" {
		t.Errorf("unexpected second group: %+v", result[1])
	}
}

func TestGroupsCreate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	created := Group{
		ID:              "g3",
		Slug:            "marketing",
		Name:            "Marketing",
		DefaultChannels: []string{"email", "inbox"},
		CreatedAt:       now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/groups" {
			t.Errorf("expected /v1/groups, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body CreateGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Slug != "marketing" || body.Name != "Marketing" {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Groups.Create(context.Background(), CreateGroupRequest{
		Slug:            "marketing",
		Name:            "Marketing",
		DefaultChannels: []string{"email", "inbox"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "g3" {
		t.Errorf("expected id g3, got %s", result.ID)
	}
	if result.Slug != "marketing" {
		t.Errorf("expected slug marketing, got %s", result.Slug)
	}
}

func TestGroupsUpdate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	newName := "Alerts Updated"
	updated := Group{
		ID:              "g1",
		Slug:            "alerts",
		Name:            newName,
		DefaultChannels: []string{"email", "sms"},
		CreatedAt:       now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/groups/g1" {
			t.Errorf("expected /v1/groups/g1, got %s", r.URL.Path)
		}

		var body UpdateGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Name == nil || *body.Name != newName {
			t.Errorf("unexpected name in body: %v", body.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	result, err := c.Groups.Update(context.Background(), "g1", UpdateGroupRequest{
		Name:            &newName,
		DefaultChannels: []string{"email", "sms"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "g1" {
		t.Errorf("expected id g1, got %s", result.ID)
	}
	if result.Name != newName {
		t.Errorf("expected name %q, got %q", newName, result.Name)
	}
}
