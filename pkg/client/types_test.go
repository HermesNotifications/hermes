// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/pkg/client"
)

func TestTemplatesList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	templates := []client.NotificationTemplate{
		{ID: "t1", Slug: "welcome", Name: "Welcome", CreatedAt: now},
		{ID: "t2", Slug: "alert", Name: "Alert", Content: map[string]map[string]string{"email": {"subject": "Alert!"}}, CreatedAt: now},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/templates" {
			t.Errorf("expected /v1/templates, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(templates)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Templates.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(result))
	}
	if result[0].ID != "t1" || result[0].Slug != "welcome" {
		t.Errorf("unexpected first template: %+v", result[0])
	}
	if result[1].ID != "t2" || result[1].Content == nil || result[1].Content["email"]["subject"] != "Alert!" {
		t.Errorf("unexpected second template: %+v", result[1])
	}
}

func TestTemplatesCreate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	created := client.NotificationTemplate{
		ID:        "t3",
		Slug:      "newsletter",
		Name:      "Newsletter",
		Content:   map[string]map[string]string{"email": {"subject": "Your weekly digest"}},
		CreatedAt: now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/templates" {
			t.Errorf("expected /v1/templates, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body client.CreateTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Slug != "newsletter" || body.Name != "Newsletter" {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Templates.Create(context.Background(), client.CreateTemplateRequest{
		Slug:    "newsletter",
		Name:    "Newsletter",
		Content: map[string]map[string]string{"email": {"subject": "Your weekly digest"}},
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
	if result.Content == nil || result.Content["email"]["subject"] != "Your weekly digest" {
		t.Errorf("unexpected content: %v", result.Content)
	}
}

func TestTemplatesUpdate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	updated := client.NotificationTemplate{
		ID:        "t1",
		Slug:      "welcome",
		Name:      "Welcome Updated",
		Content:   map[string]map[string]string{"email": {"subject": "Welcome aboard!"}},
		CreatedAt: now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/templates/t1" {
			t.Errorf("expected /v1/templates/t1, got %s", r.URL.Path)
		}

		var body client.UpdateTemplateRequest
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

	c := client.New(srv.URL, "test-key")
	result, err := c.Templates.Update(context.Background(), "t1", client.UpdateTemplateRequest{
		Name:    "Welcome Updated",
		Content: map[string]map[string]string{"email": {"subject": "Welcome aboard!"}},
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

func TestTemplatesDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/templates/t1" {
			t.Errorf("expected /v1/templates/t1, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	err := c.Templates.Delete(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
