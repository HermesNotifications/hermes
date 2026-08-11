// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package config_test

import (
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		t.Fatal("expected default DatabaseURL")
	}
	if cfg.NATSUrl == "" {
		t.Fatal("expected default NATSUrl")
	}
	if cfg.RedisURL == "" {
		t.Fatal("expected default RedisURL")
	}
	if cfg.HTTPPort == 0 {
		t.Fatal("expected default HTTPPort")
	}
	if cfg.DispatchConcurrency != 8 {
		t.Fatalf("expected default DispatchConcurrency 8, got %d", cfg.DispatchConcurrency)
	}
	if cfg.DispatchPrefetch != 64 {
		t.Fatalf("expected default DispatchPrefetch 64, got %d", cfg.DispatchPrefetch)
	}
}

// ADR 0005 phase 2. The CA bundle defaults to empty on purpose: local NATS has no
// certificate, and internal/messaging treats an empty path as "leave the connection
// alone". Deployments set it to the cert-manager CA mounted into the pod.
func TestLoad_NATSCABundle(t *testing.T) {
	if got := config.Load().NATSCABundlePath; got != "" {
		t.Fatalf("expected no CA bundle by default so make infra-up keeps working, got %q", got)
	}

	t.Setenv("HERMES_NATS_CA_BUNDLE", "/etc/nats-certs/ca.crt")
	if got := config.Load().NATSCABundlePath; got != "/etc/nats-certs/ca.crt" {
		t.Fatalf("expected the CA bundle path from the environment, got %q", got)
	}
}

// ADR 0005 phase 3. Same shape as the CA bundle, and empty for the same reason: the local
// overlay and `make infra-up` run NATS with no accounts, so there is nobody to
// authenticate as. It is not a silent downgrade — a server that defines accounts refuses an
// anonymous connection, so a deployment that forgets the seed fails to start.
func TestLoad_NATSNKeySeed(t *testing.T) {
	if got := config.Load().NATSNKeySeedPath; got != "" {
		t.Fatalf("expected no NKey seed by default so make infra-up keeps working, got %q", got)
	}

	t.Setenv("HERMES_NATS_NKEY_SEED", "/etc/nats-nkey/seed.nk")
	if got := config.Load().NATSNKeySeedPath; got != "/etc/nats-nkey/seed.nk" {
		t.Fatalf("expected the NKey seed path from the environment, got %q", got)
	}
}

func TestLoad_DispatchConcurrencyOverride(t *testing.T) {
	t.Setenv("HERMES_DISPATCH_CONCURRENCY", "12")

	cfg := config.Load()
	if cfg.DispatchConcurrency != 12 {
		t.Fatalf("expected DispatchConcurrency 12, got %d", cfg.DispatchConcurrency)
	}
}

func TestLoad_OverrideFromEnv(t *testing.T) {
	t.Setenv("HERMES_HTTP_PORT", "9999")
	t.Setenv("HERMES_DATABASE_URL", "postgres://custom:5432/hermes")

	cfg := config.Load()
	if cfg.HTTPPort != 9999 {
		t.Fatalf("expected port 9999, got %d", cfg.HTTPPort)
	}
	if cfg.DatabaseURL != "postgres://custom:5432/hermes" {
		t.Fatalf("expected custom DB URL, got %s", cfg.DatabaseURL)
	}
}

// ADR 0005 phase 1. Validate is what makes transport security legible and enforceable
// in code rather than a substring of a secret nobody can inspect. It deliberately checks
// the connection strings themselves rather than parallel "tls enabled" settings, because
// two settings that can disagree are worse than one that cannot.

func secureConfig() config.Config {
	return config.Config{
		Environment:      "production",
		DatabaseURL:      "postgres://u:p@db:5432/hermes?sslmode=verify-full",
		RedisURL:         "rediss://:token@cache:6379/0",
		NATSUrl:          "tls://nats:4222",
		JWTSecret:        "a-real-secret",
		APIKeyHMACSecret: "another-real-secret",
		CentrifugoAPIKey: "a-real-centrifugo-key",
	}
}

func TestValidate_AcceptsAFullySecureProductionConfig(t *testing.T) {
	if err := secureConfig().Validate(); err != nil {
		t.Fatalf("expected a secure production config to validate, got: %v", err)
	}
}

// The whole point of an environment gate: local development must keep working with the
// plaintext defaults that `make infra-up` provides.
func TestValidate_AllowsPlaintextInDevelopment(t *testing.T) {
	cfg := config.Load() // defaults: sslmode=disable, redis://, nats://, placeholder secrets
	if cfg.Environment != "development" {
		t.Fatalf("expected the default environment to be development, got %q", cfg.Environment)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("development config must validate, got: %v", err)
	}
}

func TestValidate_RejectsInsecureTransportOutsideDevelopment(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*config.Config)
		wantSub string
	}{
		{
			name:    "rejects a database URL with TLS disabled",
			mutate:  func(c *config.Config) { c.DatabaseURL = "postgres://u:p@db:5432/hermes?sslmode=disable" },
			wantSub: "HERMES_DATABASE_URL",
		},
		{
			name:    "rejects a database URL with no sslmode at all",
			mutate:  func(c *config.Config) { c.DatabaseURL = "postgres://u:p@db:5432/hermes" },
			wantSub: "HERMES_DATABASE_URL",
		},
		{
			name:    "rejects plaintext redis",
			mutate:  func(c *config.Config) { c.RedisURL = "redis://cache:6379/0" },
			wantSub: "HERMES_REDIS_URL",
		},
		{
			name:    "rejects plaintext nats",
			mutate:  func(c *config.Config) { c.NATSUrl = "nats://nats:4222" },
			wantSub: "HERMES_NATS_URL",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := secureConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected an error, got nil — the service would have started in the clear")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not name the offending variable %q", err, tc.wantSub)
			}
		})
	}
}

// Finding 4's second half: the placeholder secrets have no environment gate, so a service
// with no variables set comes up fully functional and trivially forgeable.
func TestValidate_RejectsPlaceholderSecretsOutsideDevelopment(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"rejects the default JWT secret", func(c *config.Config) { c.JWTSecret = "hermes-jwt-secret" }},
		{"rejects the default HMAC secret", func(c *config.Config) { c.APIKeyHMACSecret = "hermes-dev-hmac-secret" }},
		{"rejects the default Centrifugo key", func(c *config.Config) { c.CentrifugoAPIKey = "centrifugo-api-key" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := secureConfig()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected an error, got nil — a published default would be the live secret")
			}
		})
	}
}

// One report listing everything wrong beats nine restarts each revealing one more problem.
func TestValidate_ReportsEveryProblemAtOnce(t *testing.T) {
	cfg := secureConfig()
	cfg.DatabaseURL = "postgres://u:p@db:5432/hermes?sslmode=disable"
	cfg.RedisURL = "redis://cache:6379/0"
	cfg.JWTSecret = "hermes-jwt-secret"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"HERMES_DATABASE_URL", "HERMES_REDIS_URL", "HERMES_JWT_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestValidate_TreatsUnknownEnvironmentsAsProduction(t *testing.T) {
	// A typo in HERMES_ENV must not silently disable every check. Anything that is not
	// explicitly development gets the strict path.
	cfg := secureConfig()
	cfg.Environment = "prodution" // deliberate typo
	cfg.RedisURL = "redis://cache:6379/0"

	if err := cfg.Validate(); err == nil {
		t.Fatal("a misspelled environment must not fall back to the permissive path")
	}
}
