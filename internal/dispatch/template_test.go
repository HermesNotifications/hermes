// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/models"
)

func strPtr(s string) *string { return &s }

func TestRenderTemplates(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailSubject: strPtr("Invoice {{.number}} paid"),
		EmailBody:    strPtr("<p>Hi {{.name}}, invoice {{.number}} is paid.</p>"),
		InboxTitle:   strPtr("Invoice {{.number}} paid"),
		InboxBody:    strPtr("Your invoice {{.number}} has been paid."),
	}
	data := map[string]any{"number": "INV-001", "name": "Alice"}

	rc, err := dispatch.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}
	if rc.EmailSubject != "Invoice INV-001 paid" {
		t.Fatalf("email_subject: got %q", rc.EmailSubject)
	}
	if rc.InboxTitle != "Invoice INV-001 paid" {
		t.Fatalf("inbox_title: got %q", rc.InboxTitle)
	}
	if rc.InboxBody != "Your invoice INV-001 has been paid." {
		t.Fatalf("inbox_body: got %q", rc.InboxBody)
	}
}

func TestRenderTemplates_HTMLEscaping(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailBody: strPtr("<p>Hello {{.name}}</p>"),
	}
	data := map[string]any{"name": "<script>alert('xss')</script>"}
	rc, err := dispatch.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rc.EmailBody == "<p>Hello <script>alert('xss')</script></p>" {
		t.Fatal("expected HTML escaping but got raw script tag")
	}
}

func TestRenderDirectContent_WithData(t *testing.T) {
	title, body, err := dispatch.RenderDirectContent("Invoice {{.number}}", "Paid: {{.amount}}", map[string]any{"number": "123", "amount": "$99"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Invoice 123" {
		t.Fatalf("title: got %q", title)
	}
	if body != "Paid: $99" {
		t.Fatalf("body: got %q", body)
	}
}

func TestRenderDirectContent_NoData(t *testing.T) {
	title, body, err := dispatch.RenderDirectContent("Hello", "World", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Hello" || body != "World" {
		t.Fatalf("got %q %q", title, body)
	}
}
