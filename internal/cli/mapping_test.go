// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/cli"
)

// captureBody returns an httptest.Server that stores the decoded request body
// for the given path+method into *got, and responds with resp.
func captureBodyServer(t *testing.T, method, path string, respBody any, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method && r.URL.Path == path {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("reading request body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Errorf("decoding request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			*got = m
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(respBody)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// deepGet navigates a nested map[string]any using dot-separated keys and
// returns the value at the leaf, or nil if any segment is missing.
func deepGet(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func TestTemplateCreateContentMapping(t *testing.T) {
	templateResp := map[string]any{
		"id":         "tpl-1",
		"slug":       "welcome",
		"name":       "Welcome",
		"created_at": "2026-01-01T00:00:00Z",
	}

	var captured map[string]any
	srv := captureBodyServer(t, http.MethodPost, "/v1/templates", templateResp, &captured)
	defer srv.Close()

	cmd := cli.NewRootCmdForTest()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"--url", srv.URL, "--api-key", "test",
		"templates", "create",
		"--slug", "welcome",
		"--name", "Welcome",
		"--email-subject", "Hello {{.Name}}",
		"--email-body", "Welcome body",
		"--sms-body", "SMS body",
		"--inbox-title", "Inbox title",
		"--inbox-body", "Inbox body",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("no request body captured")
	}

	assertString := func(want string, keys ...string) {
		t.Helper()
		got := deepGet(captured, keys...)
		if got != want {
			t.Errorf("content[%v]: want %q, got %v", keys, want, got)
		}
	}

	assertString("Hello {{.Name}}", "content", "email", "subject")
	assertString("Welcome body", "content", "email", "body")
	assertString("SMS body", "content", "sms", "body")
	assertString("Inbox title", "content", "inbox", "title")
	assertString("Inbox body", "content", "inbox", "body")
}

func TestSendContactsMapping(t *testing.T) {
	sendResp := map[string]any{"notification_id": "notif-123"}

	var captured map[string]any
	srv := captureBodyServer(t, http.MethodPost, "/v1/send", sendResp, &captured)
	defer srv.Close()

	cmd := cli.NewRootCmdForTest()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"--url", srv.URL, "--api-key", "test",
		"notifications", "send",
		"--organization-id", "ten-1",
		"--user-id", "usr-1",
		"--template", "welcome",
		"--email", "alice@example.com",
		"--phone", "+15550001234",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("no request body captured")
	}

	assertContact := func(key, want string) {
		t.Helper()
		got := deepGet(captured, "to", "contacts", key)
		if got != want {
			t.Errorf("to.contacts[%q]: want %q, got %v", key, want, got)
		}
	}

	assertContact("email", "alice@example.com")
	assertContact("phone", "+15550001234")
}

func TestSendContactsOmittedWhenEmpty(t *testing.T) {
	sendResp := map[string]any{"notification_id": "notif-456"}

	var captured map[string]any
	srv := captureBodyServer(t, http.MethodPost, "/v1/send", sendResp, &captured)
	defer srv.Close()

	cmd := cli.NewRootCmdForTest()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"--url", srv.URL, "--api-key", "test",
		"notifications", "send",
		"--organization-id", "ten-1",
		"--user-id", "usr-1",
		"--template", "welcome",
		// intentionally no --email or --phone
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("no request body captured")
	}

	toField, ok := captured["to"]
	if !ok {
		t.Fatal("expected 'to' key in request body")
	}
	toMap, ok := toField.(map[string]any)
	if !ok {
		t.Fatalf("expected 'to' to be a map, got %T", toField)
	}
	if _, hasContacts := toMap["contacts"]; hasContacts {
		t.Errorf("expected 'to.contacts' to be absent (omitempty), but it was present: %v", toMap["contacts"])
	}
}
