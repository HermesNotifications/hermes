// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestAPIKeysService_List(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]client.APIKey{{ID: "key_abc", Name: "Test"}})
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	keys, err := c.APIKeys.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "key_abc" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestAPIKeysService_Create(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(client.APIKeyCreated{ID: "key_abc", Name: "Test", RawKey: "hms_key_abc_secret"})
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	created, err := c.APIKeys.Create(context.Background(), client.CreateAPIKeyRequest{Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if created.RawKey != "hms_key_abc_secret" {
		t.Fatalf("unexpected raw_key: %s", created.RawKey)
	}
}

func TestAPIKeysService_Delete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys/key_abc" || r.Method != http.MethodDelete {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	err := c.APIKeys.Delete(context.Background(), "key_abc")
	if err != nil {
		t.Fatal(err)
	}
}
