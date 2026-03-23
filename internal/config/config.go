package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort         int
	DatabaseURL      string
	NATSUrl          string
	RedisURL         string
	JWTSecret        string
	CentrifugoAPIURL string
	CentrifugoAPIKey string
	EmailWebhookURL  string
	SMSWebhookURL    string
	APIKeyHMACSecret string
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
		EmailWebhookURL:  envStr("HERMES_EMAIL_WEBHOOK_URL", "http://localhost:9090/email"),
		SMSWebhookURL:    envStr("HERMES_SMS_WEBHOOK_URL", "http://localhost:9090/sms"),
		APIKeyHMACSecret: envStr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
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
