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
	if p.host != "localhost" {
		t.Errorf("expected host 'localhost', got %q", p.host)
	}
	if p.port != 1025 {
		t.Errorf("expected port 1025, got %d", p.port)
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

	if p.username != "user" {
		t.Errorf("expected username 'user', got %q", p.username)
	}
	if p.password != "pass" {
		t.Errorf("expected password 'pass', got %q", p.password)
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
