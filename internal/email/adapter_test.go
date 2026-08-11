// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package email

import (
	"context"
	"strings"
	"testing"
	"text/template"

	"github.com/hermes-notifications/hermes/internal/delivery"
)

type mockEmailProvider struct {
	name      string
	lastEmail *Email
	returnID  string
	returnErr error
}

func (m *mockEmailProvider) Name() string { return m.name }
func (m *mockEmailProvider) Send(_ context.Context, e Email) (string, error) {
	m.lastEmail = &e
	return m.returnID, m.returnErr
}

func TestDeliveryAdapter_Send_MapsFields(t *testing.T) {
	mp := &mockEmailProvider{name: "test", returnID: "msg-123"}
	adapter := NewDeliveryAdapter(mp, "sender@example.com", nil)

	req := delivery.DeliveryRequest{
		NotificationID: "notif-1",
		EmailTo:        "user@example.com",
		Title:          "Test Subject",
		Body:           "<p>Hello</p>",
	}

	result, err := adapter.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.ProviderID != "msg-123" {
		t.Errorf("expected provider ID 'msg-123', got %q", result.ProviderID)
	}

	if mp.lastEmail.From != "sender@example.com" {
		t.Errorf("expected From 'sender@example.com', got %q", mp.lastEmail.From)
	}
	if mp.lastEmail.To != "user@example.com" {
		t.Errorf("expected To 'user@example.com', got %q", mp.lastEmail.To)
	}
	if mp.lastEmail.Subject != "Test Subject" {
		t.Errorf("expected Subject 'Test Subject', got %q", mp.lastEmail.Subject)
	}
	if mp.lastEmail.HTMLBody != "<p>Hello</p>" {
		t.Errorf("expected HTMLBody '<p>Hello</p>', got %q", mp.lastEmail.HTMLBody)
	}
}

func TestDeliveryAdapter_Send_EmptyEmailTo(t *testing.T) {
	mp := &mockEmailProvider{name: "test"}
	adapter := NewDeliveryAdapter(mp, "sender@example.com", nil)

	req := delivery.DeliveryRequest{
		NotificationID: "notif-1",
		Title:          "Test",
		Body:           "Body",
	}

	result, err := adapter.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty EmailTo")
	}
	if result.Success {
		t.Error("expected success=false")
	}
	if mp.lastEmail != nil {
		t.Error("provider should not be called with empty EmailTo")
	}
}

func TestDeliveryAdapter_Send_WithLayout(t *testing.T) {
	mp := &mockEmailProvider{name: "test", returnID: "msg-456"}
	layout := template.Must(template.New("layout").Parse(`<html><body>{{.Content}}</body></html>`))
	adapter := NewDeliveryAdapter(mp, "sender@example.com", layout)

	req := delivery.DeliveryRequest{
		EmailTo: "user@example.com",
		Title:   "Test",
		Body:    "<p>Hello World</p>",
	}

	_, err := adapter.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(mp.lastEmail.HTMLBody, "<p>Hello World</p>") {
		t.Error("layout should contain the original body content without escaping")
	}
	if !strings.Contains(mp.lastEmail.HTMLBody, "<html><body>") {
		t.Error("layout wrapper should be present")
	}
}

func TestDeliveryAdapter_Send_LayoutNoDoubleEscape(t *testing.T) {
	mp := &mockEmailProvider{name: "test"}
	layout := template.Must(template.New("layout").Parse(`<div>{{.Content}}</div>`))
	adapter := NewDeliveryAdapter(mp, "sender@example.com", layout)

	req := delivery.DeliveryRequest{
		EmailTo: "user@example.com",
		Title:   "Test",
		Body:    `<p>Hello &amp; World</p>`,
	}

	_, err := adapter.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// text/template should NOT escape HTML — verify no double escaping
	if strings.Contains(mp.lastEmail.HTMLBody, "&amp;amp;") {
		t.Error("layout double-escaped the HTML body")
	}
	if !strings.Contains(mp.lastEmail.HTMLBody, "&amp;") {
		t.Error("original HTML entities should be preserved")
	}
}

func TestDeliveryAdapter_Send_NilLayout(t *testing.T) {
	mp := &mockEmailProvider{name: "test"}
	adapter := NewDeliveryAdapter(mp, "sender@example.com", nil)

	req := delivery.DeliveryRequest{
		EmailTo: "user@example.com",
		Title:   "Test",
		Body:    "<p>Raw body</p>",
	}

	_, err := adapter.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mp.lastEmail.HTMLBody != "<p>Raw body</p>" {
		t.Errorf("without layout, body should pass through unchanged, got %q", mp.lastEmail.HTMLBody)
	}
}
