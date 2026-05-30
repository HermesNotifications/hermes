// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestNew(t *testing.T) {
	c := client.New("http://localhost:8080", "test-key")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := client.New("http://localhost:8080", "test-key", client.WithHTTPClient(custom))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &client.APIError{StatusCode: 400, Message: "bad input"}
	expected := "API error (400): bad input"
	if err.Error() != expected {
		t.Errorf("got %q, want %q", err.Error(), expected)
	}
}

func TestAPIErrorOnBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad input"}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	_, err := c.Categories.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected StatusCode 400, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "bad input" {
		t.Errorf("expected message %q, got %q", "bad input", apiErr.Message)
	}
}
