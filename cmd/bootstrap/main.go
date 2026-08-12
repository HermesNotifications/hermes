// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Command bootstrap creates the first API key for a fresh Hermes installation and stores it in
// a Kubernetes Secret.
//
// It exists because creating an API key requires an API key: POST /v1/apikeys demands
// apikeys:manage, so a cluster with no keys has no way to make one. Without this, a clean
// install produces a healthy cluster the operator cannot authenticate against.
//
// Safe to run repeatedly -- `helm upgrade` re-runs it on every revision. See
// internal/apikeybootstrap for the four paths it can take.
//
// Not to be confused with cmd/seed, which is a local-development tool: it writes
// web/admin/.env.local on a laptop, or writes to AWS Secrets Manager. Neither is usable on a
// generic cluster, which is why this is a separate binary rather than a third mode there.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hermesnotifications/hermes/internal/apikeybootstrap"
)

func main() {
	secretName := flag.String("secret-name", os.Getenv("HERMES_BOOTSTRAP_SECRET_NAME"),
		"Kubernetes Secret to hold the key")
	keyName := flag.String("key-name", envOr("HERMES_BOOTSTRAP_KEY_NAME", "Bootstrap"),
		"Label stored on the api_keys row")
	flag.Parse()

	dbURL := os.Getenv("HERMES_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("HERMES_DATABASE_URL is required")
	}
	hmacSecret := os.Getenv("HERMES_API_KEY_HMAC_SECRET")
	if hmacSecret == "" {
		// Without it the hash is computed against the empty string and no key ever verifies —
		// a bootstrap that appears to succeed and produces an unusable credential.
		log.Fatal("HERMES_API_KEY_HMAC_SECRET is required; without it the stored hash is unverifiable")
	}

	// An operator-supplied key: the Job then only ensures the database row and needs no RBAC
	// to write Secrets at all, which is the path for clusters that forbid that.
	existingKey := os.Getenv("HERMES_BOOTSTRAP_EXISTING_KEY")
	if existingKey == "" && *secretName == "" {
		log.Fatal("either -secret-name or HERMES_BOOTSTRAP_EXISTING_KEY must be set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	// The Job runs alongside the migration Job rather than after it (ADR 0008), so api_keys may
	// not exist yet on a first install. Failing here is correct and expected: backoffLimit gives
	// us the retries, and Kubernetes is a better retry loop than one written inline.
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("database not reachable (has the migration Job run?): %v", err)
	}

	opts := apikeybootstrap.Options{
		SecretName:  *secretName,
		KeyName:     *keyName,
		HMACSecret:  hmacSecret,
		ExistingKey: existingKey,
	}

	var secrets apikeybootstrap.SecretStore
	if existingKey == "" {
		kube, err := apikeybootstrap.NewKubeSecrets()
		if err != nil {
			log.Fatalf("build Kubernetes client: %v", err)
		}
		secrets = kube
	}

	outcome, err := apikeybootstrap.Run(ctx, apikeybootstrap.NewPostgresKeys(pool), secrets, opts)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	// The key id, never the key. Job pods are retained on purpose (see NOTES.txt), so anything
	// printed here outlives the run and lands in whatever collects logs.
	switch outcome.Action {
	case "created":
		log.Printf("created API key %s and wrote it to secret %s", outcome.KeyID, *secretName)
	case "present":
		log.Printf("API key %s already present; nothing to do", outcome.KeyID)
	case "readopted":
		log.Printf("re-inserted API key %s from secret %s (the database had no row for it)",
			outcome.KeyID, *secretName)
	case "supplied":
		log.Printf("inserted the supplied API key %s", outcome.KeyID)
	case "raced":
		log.Printf("another bootstrap won the race; key %s is orphaned and can be deleted", outcome.KeyID)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
