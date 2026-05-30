// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package email

import (
	"context"
	"fmt"
	"sync"

	"github.com/wneessen/go-mail"
)

// SMTPProvider sends emails via SMTP using go-mail with a persistent connection.
type SMTPProvider struct {
	client    *mail.Client
	connected bool
	mu        sync.Mutex
}

// NewSMTPProvider creates a new SMTP email provider with a persistent client.
// The connection is established lazily on the first Send call.
func NewSMTPProvider(cfg Config) *SMTPProvider {
	opts := []mail.Option{
		mail.WithPort(cfg.SMTPPort),
	}

	if cfg.SMTPUsername != "" && cfg.SMTPPassword != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.SMTPUsername),
			mail.WithPassword(cfg.SMTPPassword),
		)
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}

	client, err := mail.NewClient(cfg.SMTPHost, opts...)
	if err != nil {
		// NewClient only fails on invalid options; treat as panic since it's a
		// programming error (bad config) caught at startup.
		panic(fmt.Sprintf("smtp: create client: %v", err))
	}

	return &SMTPProvider{
		client: client,
	}
}

func (s *SMTPProvider) Name() string { return "smtp" }

func (s *SMTPProvider) Send(ctx context.Context, e Email) (string, error) {
	msg, err := buildMsg(e)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		if err := s.client.DialWithContext(ctx); err != nil {
			return "", fmt.Errorf("smtp dial: %w", err)
		}
		s.connected = true
	}

	if err := s.client.Send(msg); err != nil {
		// Connection may be stale (server closed idle connection). Close, redial, retry once.
		_ = s.client.Close()
		s.connected = false

		if err := s.client.DialWithContext(ctx); err != nil {
			return "", fmt.Errorf("smtp redial: %w", err)
		}
		s.connected = true

		if err := s.client.Send(msg); err != nil {
			return "", fmt.Errorf("send email: %w", err)
		}
	}

	return "", nil
}

// Close closes the underlying SMTP connection. It should be called on shutdown.
func (s *SMTPProvider) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected {
		s.connected = false
		return s.client.Close()
	}
	return nil
}

func buildMsg(e Email) (*mail.Msg, error) {
	msg := mail.NewMsg()
	if err := msg.From(e.From); err != nil {
		return nil, fmt.Errorf("set from: %w", err)
	}
	if err := msg.To(e.To); err != nil {
		return nil, fmt.Errorf("set to: %w", err)
	}
	msg.Subject(e.Subject)

	if e.ReplyTo != "" {
		if err := msg.ReplyTo(e.ReplyTo); err != nil {
			return nil, fmt.Errorf("set reply-to: %w", err)
		}
	}

	if e.HTMLBody != "" {
		msg.SetBodyString(mail.TypeTextHTML, e.HTMLBody)
	}
	if e.TextBody != "" {
		if e.HTMLBody != "" {
			msg.AddAlternativeString(mail.TypeTextPlain, e.TextBody)
		} else {
			msg.SetBodyString(mail.TypeTextPlain, e.TextBody)
		}
	}

	return msg, nil
}
