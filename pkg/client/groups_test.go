// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestCategoriesList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	categories := []client.SubscriptionCategory{
		{ID: "sct-1", Slug: "account", Name: "Account", DefaultChannels: []string{"email"}, DefaultState: "required", CreatedAt: now},
		{ID: "sct-2", Slug: "general", Name: "General", DefaultChannels: []string{"email", "inbox"}, DefaultState: "on", CreatedAt: now},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/subscriptions/categories" {
			t.Errorf("expected /v1/subscriptions/categories, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(categories)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Categories.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(result))
	}
	if result[0].ID != "sct-1" || result[0].Slug != "account" {
		t.Errorf("unexpected first category: %+v", result[0])
	}
	if result[1].ID != "sct-2" || result[1].Slug != "general" {
		t.Errorf("unexpected second category: %+v", result[1])
	}
}

func TestCategoriesCreate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	created := client.SubscriptionCategory{
		ID:              "sct-3",
		Slug:            "marketing",
		Name:            "Marketing",
		DefaultChannels: []string{"email"},
		DefaultState:    "off",
		CreatedAt:       now,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/subscriptions/categories" {
			t.Errorf("expected /v1/subscriptions/categories, got %s", r.URL.Path)
		}

		var body client.CreateCategoryRequest
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

	c := client.New(srv.URL, "test-key")
	result, err := c.Categories.Create(context.Background(), client.CreateCategoryRequest{
		Slug:            "marketing",
		Name:            "Marketing",
		DefaultChannels: []string{"email"},
		DefaultState:    "off",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "sct-3" {
		t.Errorf("expected id sct-3, got %s", result.ID)
	}
	if result.Slug != "marketing" {
		t.Errorf("expected slug marketing, got %s", result.Slug)
	}
}
