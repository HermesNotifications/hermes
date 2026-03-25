package email

import (
	"context"
	"fmt"
)

// Email represents a single email message to be sent.
type Email struct {
	From     string
	To       string
	Subject  string
	HTMLBody string
	TextBody string
	ReplyTo  string
}

// Provider is the interface for email delivery backends.
type Provider interface {
	Send(ctx context.Context, email Email) (providerID string, err error)
	Name() string
}

// Config holds email provider configuration.
type Config struct {
	Provider     string
	From         string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SESRegion    string
	LayoutPath   string
}

// NewProvider creates an email Provider based on the config.
func NewProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "smtp":
		return NewSMTPProvider(cfg), nil
	case "ses":
		return NewSESProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown email provider: %q", cfg.Provider)
	}
}
