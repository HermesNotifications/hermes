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

func TestInboxClientList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	listResp := client.InboxListResponse{
		Data: []client.InboxNotification{
			{ID: "n1", Title: "Hello", Status: "delivered", CreatedAt: now},
			{ID: "n2", Title: "World", Status: "read", CreatedAt: now},
		},
		UnreadCount: 1,
		Cursor:      "abc123",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			t.Errorf("expected Bearer test-jwt, got %s", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("archived") != "false" {
			t.Errorf("expected archived=false, got %s", r.URL.Query().Get("archived"))
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("expected limit=20, got %s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listResp)
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	result, err := c.List(context.Background(), false, "", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(result.Data))
	}
	if result.UnreadCount != 1 {
		t.Errorf("expected unread_count 1, got %d", result.UnreadCount)
	}
	if result.Cursor != "abc123" {
		t.Errorf("expected cursor abc123, got %s", result.Cursor)
	}
}

func TestInboxClientMarkRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/inbox/n1/read" {
			t.Errorf("expected /v1/inbox/n1/read, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	if err := c.MarkRead(context.Background(), "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboxClientMarkUnread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/inbox/n1/read" {
			t.Errorf("expected /v1/inbox/n1/read, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	if err := c.MarkUnread(context.Background(), "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboxClientArchive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/inbox/n1/archive" {
			t.Errorf("expected /v1/inbox/n1/archive, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	if err := c.Archive(context.Background(), "n1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboxClientMarkAllRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/v1/inbox/read-all" {
			t.Errorf("expected /v1/inbox/read-all, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	if err := c.MarkAllRead(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboxClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"notification not found"}`))
	}))
	defer srv.Close()

	c := client.NewInboxClient(srv.URL, "test-jwt")
	err := c.MarkRead(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
}
