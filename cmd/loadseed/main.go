// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/hermesnotifications/hermes/internal/database"
)

type Config struct {
	Organizations                  int
	UsersPerOrganization           int
	CategoriesPerOrganization      int
	SubscriptionsPerCategory int
	TemplatesPerSubscription int
	DatabaseURL              string
	AdminURL                 string
	HMACSecret               string
	OutputPath               string
	Cleanup                  bool
}

func main() {
	cfg := parseFlags()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if cfg.Cleanup {
		if err := runCleanup(ctx, pool, cfg); err != nil {
			log.Fatalf("cleanup: %v", err)
		}
		return
	}
	if err := runSeed(ctx, pool, cfg); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func parseFlags() Config {
	var cfg Config
	flag.IntVar(&cfg.Organizations, "organizations", 10, "number of organizations")
	flag.IntVar(&cfg.UsersPerOrganization, "users-per-organization", 10000, "users per organization")
	flag.IntVar(&cfg.CategoriesPerOrganization, "categories-per-organization", 3, "subscription categories per organization")
	flag.IntVar(&cfg.SubscriptionsPerCategory, "subscriptions-per-category", 2, "subscriptions per category")
	flag.IntVar(&cfg.TemplatesPerSubscription, "templates-per-subscription", 2, "templates per subscription")
	flag.StringVar(&cfg.DatabaseURL, "database-url", os.Getenv("HERMES_DATABASE_URL"), "Postgres URL")
	flag.StringVar(&cfg.AdminURL, "admin-url", "http://localhost:8080", "admin base URL (warm-up only)")
	flag.StringVar(&cfg.HMACSecret, "hmac-secret", envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"), "HMAC secret for api-key hashing")
	flag.StringVar(&cfg.OutputPath, "output", "loadtest/seed-manifest.json", "manifest output path")
	flag.BoolVar(&cfg.Cleanup, "cleanup", false, "delete all seeded entities from the manifest")
	flag.Parse()

	if cfg.DatabaseURL == "" {
		log.Fatal("database-url is required (or HERMES_DATABASE_URL)")
	}
	return cfg
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// runID returns a short hex string suitable for tagging this seed run.
func runID() string { return uuid.NewString()[:8] }

