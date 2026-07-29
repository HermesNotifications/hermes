// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type EmailConfig struct {
	Provider     string
	From         string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SESRegion    string
	LayoutPath   string
}

type Config struct {
	HTTPPort           int
	DatabaseURL        string
	NATSUrl            string
	RedisURL           string
	JWTSecret          string
	CentrifugoAPIURL   string
	CentrifugoAPIKey   string
	Email              EmailConfig
	SMSWebhookURL      string
	APIKeyHMACSecret   string
	EventRetentionDays int

	// DispatchConcurrency is the size of the dispatch worker pool — how many
	// notification.send messages are processed in parallel. Distinct notifications
	// are independent (per-notification status rollup is monotonic downstream), so
	// they can be processed in parallel to lift dispatch throughput.
	DispatchConcurrency int
	// DispatchPrefetch is the dispatch fetcher's in-flight buffer (PullMaxMessages)
	// that feeds the worker pool. Decouples fetching from processing so the pull
	// pipeline stays full without hoarding the backlog. Tunable for load-test sweeps.
	DispatchPrefetch int

	// DynamoDB / ExtendDB — set DynamoEndpoint to an ExtendDB URL for local dev and
	// multi-cloud environments; leave empty to use native DynamoDB on AWS.
	DynamoEndpoint string
	DynamoRegion   string

	// Environment gates the transport-security and placeholder-secret checks in
	// Validate. Only the exact value "development" relaxes them — see ADR 0005.
	Environment string
}

func Load() Config {
	return Config{
		HTTPPort:         envInt("HERMES_HTTP_PORT", 8080),
		DatabaseURL:      envStr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		NATSUrl:          envStr("HERMES_NATS_URL", "nats://localhost:4222"),
		RedisURL:         envStr("HERMES_REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:        envStr("HERMES_JWT_SECRET", "hermes-jwt-secret"),
		CentrifugoAPIURL: envStr("HERMES_CENTRIFUGO_API_URL", "http://localhost:8000"),
		CentrifugoAPIKey: envStr("HERMES_CENTRIFUGO_API_KEY", "centrifugo-api-key"),
		Email: EmailConfig{
			Provider:     envStr("HERMES_EMAIL_PROVIDER", "smtp"),
			From:         envStr("HERMES_EMAIL_FROM", "noreply@example.com"),
			SMTPHost:     envStr("HERMES_EMAIL_SMTP_HOST", "localhost"),
			SMTPPort:     envInt("HERMES_EMAIL_SMTP_PORT", 1025),
			SMTPUsername: envStr("HERMES_EMAIL_SMTP_USERNAME", ""),
			SMTPPassword: envStr("HERMES_EMAIL_SMTP_PASSWORD", ""),
			SESRegion:    envStr("HERMES_EMAIL_SES_REGION", "us-east-1"),
			LayoutPath:   envStr("HERMES_EMAIL_LAYOUT_PATH", ""),
		},
		SMSWebhookURL:       envStr("HERMES_SMS_WEBHOOK_URL", "http://localhost:9090/sms"),
		APIKeyHMACSecret:    envStr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
		EventRetentionDays:  envInt("HERMES_EVENT_RETENTION_DAYS", 90),
		DispatchConcurrency: envInt("HERMES_DISPATCH_CONCURRENCY", 8),
		DispatchPrefetch:    envInt("HERMES_DISPATCH_PREFETCH", 64),
		DynamoEndpoint:      envStr("HERMES_DYNAMO_ENDPOINT", ""),
		DynamoRegion:        envStr("HERMES_DYNAMO_REGION", "us-east-1"),
		Environment:         envStr("HERMES_ENV", EnvDevelopment),
	}
}

// EnvDevelopment is the one environment in which plaintext transports and the placeholder
// secrets below are tolerated. Any other value — including a misspelling — takes the
// strict path, so a typo in HERMES_ENV cannot silently disable every check.
const EnvDevelopment = "development"

// placeholderSecrets pairs each secret with the built-in default it must not still be.
// Those defaults are committed to a public repository, so a deployment still using one
// does not have a weak secret — it has a published constant. The variable name travels
// with the check so the error can say what to fix.
var placeholderSecrets = []struct {
	envVar      string
	get         func(Config) string
	placeholder string
}{
	{"HERMES_JWT_SECRET", func(c Config) string { return c.JWTSecret }, "hermes-jwt-secret"},
	{"HERMES_API_KEY_HMAC_SECRET", func(c Config) string { return c.APIKeyHMACSecret }, "hermes-dev-hmac-secret"},
	{"HERMES_CENTRIFUGO_API_KEY", func(c Config) string { return c.CentrifugoAPIKey }, "centrifugo-api-key"},
}

// MustLoad loads configuration and exits if it is not fit for the environment it names.
//
// Failing closed at startup is the point (ADR 0005): a service that cannot reach its
// datastore securely refuses to run, rather than connecting in the clear and continuing
// to serve. That failure is loud and recoverable; the alternative is silent and is not.
func MustLoad() Config {
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	return cfg
}

// Validate reports every problem it finds, not just the first.
//
// It inspects the connection strings themselves rather than separate "TLS enabled"
// settings. Two settings that can disagree are worse than one that cannot: the URL is
// what actually governs the connection, so the URL is what gets checked.
func (c Config) Validate() error {
	if c.Environment == EnvDevelopment {
		return nil
	}

	var problems []string

	// Postgres carries TLS in the sslmode query parameter. Absent is not a safe default:
	// libpq's "prefer" falls back to plaintext without telling anyone.
	if mode := sslMode(c.DatabaseURL); !secureSSLModes[mode] {
		if mode == "" {
			problems = append(problems, "HERMES_DATABASE_URL has no sslmode; require, verify-ca or verify-full is needed outside development")
		} else {
			problems = append(problems, fmt.Sprintf("HERMES_DATABASE_URL has sslmode=%s; require, verify-ca or verify-full is needed outside development", mode))
		}
	}

	if !strings.HasPrefix(c.RedisURL, "rediss://") {
		problems = append(problems, "HERMES_REDIS_URL is not rediss://; TLS is required outside development")
	}

	// NATS accepts tls:// to enable TLS; nats:// is plaintext.
	if !strings.HasPrefix(c.NATSUrl, "tls://") {
		problems = append(problems, "HERMES_NATS_URL is not tls://; TLS is required outside development")
	}

	for _, s := range placeholderSecrets {
		if s.get(c) == s.placeholder {
			problems = append(problems, fmt.Sprintf("%s is still the built-in default, which is committed to a public repository", s.envVar))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("environment %q requires:\n  - %s", c.Environment, strings.Join(problems, "\n  - "))
}

// secureSSLModes are the libpq modes that actually encrypt. "allow" and "prefer" are
// excluded deliberately: both silently fall back to plaintext.
var secureSSLModes = map[string]bool{
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// sslMode extracts the sslmode parameter, returning "" when absent or unparseable.
func sslMode(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("sslmode")
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
