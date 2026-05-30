// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package email

import (
	"testing"
)

func TestNewSMTPProvider_DefaultConfig(t *testing.T) {
	cfg := Config{
		Provider: "smtp",
		SMTPHost: "localhost",
		SMTPPort: 1025,
	}
	p := NewSMTPProvider(cfg)

	if p.Name() != "smtp" {
		t.Errorf("expected name 'smtp', got %q", p.Name())
	}
	if p.client == nil {
		t.Error("expected non-nil client")
	}
}

func TestNewSMTPProvider_WithAuth(t *testing.T) {
	cfg := Config{
		Provider:     "smtp",
		SMTPHost:     "smtp.example.com",
		SMTPPort:     587,
		SMTPUsername: "user",
		SMTPPassword: "pass",
	}
	p := NewSMTPProvider(cfg)

	if p.client == nil {
		t.Error("expected non-nil client")
	}
}

func TestNewProvider_SMTP(t *testing.T) {
	cfg := Config{Provider: "smtp", SMTPHost: "localhost", SMTPPort: 1025}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "smtp" {
		t.Errorf("expected 'smtp', got %q", p.Name())
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	cfg := Config{Provider: "carrier-pigeon"}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
