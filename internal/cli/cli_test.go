// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/cli"
)

func TestCategoriesListTableOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/subscriptions/categories" && r.Method == "GET" {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "sct-1", "slug": "general", "name": "General", "default_channels": []string{"email"}, "default_state": "on", "created_at": "2026-01-01T00:00:00Z"},
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	cmd := cli.NewRootCmdForTest()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--url", srv.URL, "--api-key", "test", "categories", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("general")) {
		t.Errorf("expected 'general' in output, got: %s", out.String())
	}
}

func TestCategoriesListJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "sct-1", "slug": "general", "name": "General", "default_channels": []string{"email"}, "default_state": "on", "created_at": "2026-01-01T00:00:00Z"},
		})
	}))
	defer srv.Close()

	cmd := cli.NewRootCmdForTest()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--url", srv.URL, "--api-key", "test", "-o", "json", "categories", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("general")) {
		t.Errorf("expected 'general' in JSON output, got: %s", out.String())
	}
}
