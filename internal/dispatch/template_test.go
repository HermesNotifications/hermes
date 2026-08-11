// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestRenderTemplates(t *testing.T) {
	nt := &models.NotificationTemplate{
		Content: map[string]map[string]string{
			"email": {"subject": "Invoice {{.number}} paid", "body": "<p>Hi {{.name}}, invoice {{.number}} is paid.</p>"},
			"inbox": {"title": "Invoice {{.number}} paid", "body": "Your invoice {{.number}} has been paid."},
		},
	}
	data := map[string]any{"number": "INV-001", "name": "Alice"}

	rc, err := dispatch.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}
	if rc["email"]["subject"] != "Invoice INV-001 paid" {
		t.Fatalf("email subject: got %q", rc["email"]["subject"])
	}
	if rc["inbox"]["title"] != "Invoice INV-001 paid" {
		t.Fatalf("inbox title: got %q", rc["inbox"]["title"])
	}
	if rc["inbox"]["body"] != "Your invoice INV-001 has been paid." {
		t.Fatalf("inbox body: got %q", rc["inbox"]["body"])
	}
}

func TestRenderTemplates_HTMLEscaping(t *testing.T) {
	nt := &models.NotificationTemplate{
		Content: map[string]map[string]string{"email": {"body": "<p>Hello {{.name}}</p>"}},
	}
	data := map[string]any{"name": "<script>alert('xss')</script>"}
	rc, err := dispatch.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rc["email"]["body"] == "<p>Hello <script>alert('xss')</script></p>" {
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
