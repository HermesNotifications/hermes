# Email Channel Support Design

## Context

Hermes currently delegates email delivery to an external webhook endpoint (`HERMES_EMAIL_WEBHOOK_URL`). This requires operators to run a separate adapter service that receives the webhook POST and forwards it to their email provider. This design adds native email sending with configurable providers (SMTP and AWS SES), removing the need for an external adapter. It also adds Mailpit to the local dev environment for end-to-end email testing.

## Design

### Recipient resolution in Dispatch

Currently, `DeliveryMessage` carries only `UserID` — workers have no way to know the recipient's email or phone. The dispatch service must resolve user contact info and include it in the delivery message.

Changes to `internal/nats/messages.go`:

```go
type DeliveryMessage struct {
    // ... existing fields ...
    Recipient  Recipient       `json:"recipient"`
}

type Recipient struct {
    Email string `json:"email,omitempty"`
    Phone string `json:"phone,omitempty"`
}
```

Changes to `internal/dispatch/dispatch.go`:
- Add `store.UserRepository` as a dependency
- Before fanning out, call `GetUserByID(ctx, msg.UserID)` to resolve the user
- Populate `Recipient.Email` and `Recipient.Phone` from the user record
- If email channel is selected but user has no email, skip that channel and publish a `routing.no_contact` event

Changes to `internal/delivery/worker.go`:
- Map `msg.Recipient.Email` → `req.EmailTo` and `msg.Recipient.Phone` → `req.PhoneTo`

This resolves contact info once in dispatch rather than requiring each worker to have a database connection.

### Package: `internal/email/`

New package owning the email provider abstraction, implementations, and delivery adapter.

#### Core types (`email.go`)

```go
type Email struct {
    From     string
    To       string
    Subject  string
    HTMLBody string
    TextBody string // optional plain-text fallback
    ReplyTo  string // optional
}

type Provider interface {
    Send(ctx context.Context, email Email) (providerID string, err error)
    Name() string
}

type Config struct {
    Provider     string // "smtp" or "ses"
    From         string
    SMTPHost     string
    SMTPPort     int
    SMTPUsername string
    SMTPPassword string
    SESRegion    string
    LayoutPath   string // path to custom HTML layout, empty = embedded default, "none" = no layout
}

func NewProvider(cfg Config) (Provider, error) // factory
```

#### SMTP provider (`smtp.go`)

Uses `github.com/wneessen/go-mail` for correct STARTTLS negotiation, TLS support, and MIME multipart construction. Constructs a multipart MIME message (text/html + optional text/plain). Authenticates with PLAIN auth when username/password are configured; skips auth when they are empty (as with Mailpit). `go-mail` handles STARTTLS negotiation automatically, which is required by all production SMTP providers (port 587).

Returns an empty `providerID` since SMTP doesn't return message IDs.

#### SES provider (`ses.go`)

Uses AWS SDK v2 `github.com/aws/aws-sdk-go-v2/service/sesv2`. Calls `SendEmail` with HTML body and optional text body. Credentials are resolved via the standard AWS credential chain (env vars, IAM role, etc.). Returns the SES `MessageId` as `providerID`.

#### Delivery adapter (`adapter.go`)

Wraps `email.Provider` to satisfy `delivery.Provider`:

```go
type DeliveryAdapter struct {
    provider Provider
    from     string
    layout   *template.Template // text/template, nil if layout disabled
}

func NewDeliveryAdapter(provider Provider, from string, layout *template.Template) *DeliveryAdapter
```

The adapter's `Send` method:
1. Validates `EmailTo` is non-empty; returns error if missing (worker publishes `email.failed` event)
2. Maps `DeliveryRequest` fields: `Title` → `Subject`, `Body` → `HTMLBody`, `EmailTo` → `To`
3. If layout is configured, executes the layout template with `.Content` set to the rendered HTML body. The layout template uses `text/template` (not `html/template`) because the body content is already HTML-safe (rendered by dispatch using `html/template`). This avoids double-escaping.
4. Calls `provider.Send()` with the constructed `Email`
5. Returns `DeliveryResult` with the provider name and provider ID

#### HTML layout (`layout.html`)

A default HTML email layout embedded via `//go:embed layout.html`. Contains a `{{.Content}}` placeholder where the rendered `email_body` template output is injected. The layout provides minimal, responsive HTML structure.

Configuration:
- `HERMES_EMAIL_LAYOUT_PATH` empty → use embedded default
- `HERMES_EMAIL_LAYOUT_PATH` = `"none"` → no layout wrapping, raw email_body sent as-is
- `HERMES_EMAIL_LAYOUT_PATH` = file path → load custom layout from disk

### Configuration changes (`internal/config/config.go`)

Add `Email` struct field to `Config`:

```go
type EmailConfig struct {
    Provider     string `env:"HERMES_EMAIL_PROVIDER" default:"smtp"`
    From         string `env:"HERMES_EMAIL_FROM" default:"noreply@example.com"`
    SMTPHost     string `env:"HERMES_EMAIL_SMTP_HOST" default:"localhost"`
    SMTPPort     int    `env:"HERMES_EMAIL_SMTP_PORT" default:"1025"`
    SMTPUsername string `env:"HERMES_EMAIL_SMTP_USERNAME"`
    SMTPPassword string `env:"HERMES_EMAIL_SMTP_PASSWORD"`
    SESRegion    string `env:"HERMES_EMAIL_SES_REGION" default:"us-east-1"`
    LayoutPath   string `env:"HERMES_EMAIL_LAYOUT_PATH"`
}
```

SMTP defaults (localhost:1025) match Mailpit for zero-config local dev.

Remove `EmailWebhookURL` from config since the webhook provider is being replaced.

### Worker changes (`cmd/worker-email/main.go`)

Replace `WebhookProvider` with the new email provider:

```go
emailProvider, err := email.NewProvider(cfg.Email)
if err != nil {
    logger.Error("create email provider", "error", err)
    os.Exit(1)
}

layout := email.MustLoadLayout(cfg.Email.LayoutPath, logger)
adapter := email.NewDeliveryAdapter(emailProvider, cfg.Email.From, layout)

worker := delivery.NewWorker(natsClient, adapter, "email", "worker-email", logger)
```

### Infrastructure: Mailpit (`docker-compose.yml`)

Add Mailpit container for local email testing (actively maintained MailHog replacement with ARM64 support):

```yaml
mailpit:
  image: axllent/mailpit:latest
  ports:
    - "1025:1025"   # SMTP server
    - "8025:8025"   # Web UI for viewing emails
```

Developers can view captured emails at `http://localhost:8025`.

### Files to create

| File | Purpose |
|------|---------|
| `internal/email/email.go` | Email struct, Provider interface, Config, factory |
| `internal/email/smtp.go` | SMTP provider using `go-mail` |
| `internal/email/ses.go` | SES provider using AWS SDK v2 `sesv2` |
| `internal/email/adapter.go` | DeliveryAdapter wrapping email.Provider → delivery.Provider |
| `internal/email/layout.html` | Default embedded HTML email layout |
| `internal/email/layout.go` | Layout loading logic (embed + file override) |

### Files to modify

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `EmailConfig` struct, remove `EmailWebhookURL` |
| `cmd/worker-email/main.go` | Use email provider + adapter instead of WebhookProvider |
| `docker-compose.yml` | Add Mailpit service |
| `go.mod` | Add `github.com/wneessen/go-mail` and `github.com/aws/aws-sdk-go-v2/service/sesv2` |
| `internal/nats/messages.go` | Add `Recipient` struct and field to `DeliveryMessage` |
| `internal/dispatch/dispatch.go` | Add `UserRepository` dep, resolve user contact info before fan-out |
| `cmd/dispatch/main.go` | Wire `UserRepository` into `Dispatch` |
| `internal/delivery/worker.go` | Map `msg.Recipient` fields to `DeliveryRequest` |

### Files unchanged

- `internal/delivery/provider.go` — `Provider` interface stays the same
- `internal/delivery/webhook.go` — kept for SMS worker, but no longer used by email
- Database schema — no migrations needed (`email_subject`, `email_body` fields already exist, `users.email` field exists)

## Testing

### Unit tests

- `internal/email/smtp_test.go` — Test SMTP message construction, auth handling (with/without credentials)
- `internal/email/ses_test.go` — Test SES client construction and request mapping
- `internal/email/adapter_test.go` — Test DeliveryRequest → Email mapping, layout wrapping (verify no double-escaping), empty EmailTo validation, result mapping
- `internal/dispatch/dispatch_test.go` — Test user contact resolution and Recipient population in DeliveryMessage

Mock the `email.Provider` interface for adapter tests.

### Integration tests

- `internal/email/smtp_integration_test.go` (`//go:build integration`) — Send email via SMTP to Mailpit, verify delivery via Mailpit HTTP API (`GET http://localhost:8025/api/v1/messages`)

### E2E tests

- Extend `tests/e2e/pipeline_test.go` or add `tests/e2e/email_test.go` — Send notification through full pipeline (Admin API → Dispatch → Email Worker → Mailpit), verify email arrived with correct subject, body, and recipient via Mailpit API.
