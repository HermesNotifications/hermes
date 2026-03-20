package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort    int
	DatabaseURL string
	NATSUrl     string
	RedisURL    string
}

func Load() Config {
	return Config{
		HTTPPort:    envInt("HERMES_HTTP_PORT", 8080),
		DatabaseURL: envStr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		NATSUrl:     envStr("HERMES_NATS_URL", "nats://localhost:4222"),
		RedisURL:    envStr("HERMES_REDIS_URL", "redis://localhost:6379/0"),
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
