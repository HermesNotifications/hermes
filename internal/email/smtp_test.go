// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package email

import (
	"testing"

	"github.com/wneessen/go-mail"
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

// Finding 13. Credentials and transport security are independent concerns, but the
// constructor treated them as one: with no username or password it disabled TLS
// outright, so any deployment pointing HERMES_EMAIL_SMTP_HOST at a real relay
// without credentials sent mail in the clear and said nothing.
//
// Opportunistic rather than mandatory is deliberate. It upgrades via STARTTLS only
// when the server advertises it, so the local MailHog default (localhost:1025,
// no STARTTLS, no credentials — internal/config/config.go:63-66) keeps working.
func TestNewSMTPProvider_TLSPolicy(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want mail.TLSPolicy
	}{
		{
			name: "upgrades opportunistically when no credentials are configured",
			cfg:  Config{Provider: "smtp", SMTPHost: "relay.example.com", SMTPPort: 587},
			want: mail.TLSOpportunistic,
		},
		{
			name: "upgrades opportunistically for the local MailHog default",
			cfg:  Config{Provider: "smtp", SMTPHost: "localhost", SMTPPort: 1025},
			want: mail.TLSOpportunistic,
		},
		{
			name: "leaves the go-mail default in place when credentials are configured",
			cfg: Config{
				Provider: "smtp", SMTPHost: "relay.example.com", SMTPPort: 587,
				SMTPUsername: "user", SMTPPassword: "pass",
			},
			want: mail.TLSMandatory,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewSMTPProvider(tc.cfg).client.TLSPolicy()
			if got != tc.want.String() {
				t.Errorf("TLS policy = %q, want %q", got, tc.want.String())
			}
			if got == mail.NoTLS.String() {
				t.Error("TLS is disabled outright; mail would be sent in the clear")
			}
		})
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
