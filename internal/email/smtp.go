package email

import (
	"context"
	"fmt"

	"github.com/wneessen/go-mail"
)

// SMTPProvider sends emails via SMTP using go-mail.
type SMTPProvider struct {
	host     string
	port     int
	username string
	password string
}

// NewSMTPProvider creates a new SMTP email provider.
func NewSMTPProvider(cfg Config) *SMTPProvider {
	return &SMTPProvider{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
	}
}

func (s *SMTPProvider) Name() string { return "smtp" }

func (s *SMTPProvider) Send(ctx context.Context, e Email) (string, error) {
	msg := mail.NewMsg()
	if err := msg.From(e.From); err != nil {
		return "", fmt.Errorf("set from: %w", err)
	}
	if err := msg.To(e.To); err != nil {
		return "", fmt.Errorf("set to: %w", err)
	}
	msg.Subject(e.Subject)

	if e.ReplyTo != "" {
		if err := msg.ReplyTo(e.ReplyTo); err != nil {
			return "", fmt.Errorf("set reply-to: %w", err)
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

	opts := []mail.Option{
		mail.WithPort(s.port),
	}

	if s.username != "" && s.password != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.username),
			mail.WithPassword(s.password),
		)
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}

	client, err := mail.NewClient(s.host, opts...)
	if err != nil {
		return "", fmt.Errorf("create smtp client: %w", err)
	}

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return "", fmt.Errorf("send email: %w", err)
	}

	return "", nil
}
