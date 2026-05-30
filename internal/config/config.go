// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package config

import (
	"os"
	"strconv"
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
	HTTPPort         int
	DatabaseURL      string
	NATSUrl          string
	RedisURL         string
	JWTSecret        string
	CentrifugoAPIURL string
	CentrifugoAPIKey string
	Email            EmailConfig
	SMSWebhookURL    string
	APIKeyHMACSecret   string
	EventRetentionDays int
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
		SMSWebhookURL:    envStr("HERMES_SMS_WEBHOOK_URL", "http://localhost:9090/sms"),
		APIKeyHMACSecret:   envStr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
		EventRetentionDays: envInt("HERMES_EVENT_RETENTION_DAYS", 90),
	}
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
