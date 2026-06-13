// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	retentionDays := flag.Int("retention-days", envIntDefault("HERMES_EVENT_RETENTION_DAYS", 90), "Number of days to retain notification events")
	batchSize := flag.Int("batch-size", 5000, "Number of rows to delete per batch")
	flag.Parse()

	// When DynamoDB is active (HERMES_DYNAMO_ENDPOINT is set), events live in
	// hermes-events and expire automatically via native DynamoDB TTL. The Postgres
	// notification_events table is not the source of truth, so this job is a no-op.
	if os.Getenv("HERMES_DYNAMO_ENDPOINT") != "" {
		log.Println("HERMES_DYNAMO_ENDPOINT is set: events are DynamoDB TTL-managed; nothing to do")
		os.Exit(0)
	}

	if *dbURL == "" {
		log.Fatal("database-url is required (or set HERMES_DATABASE_URL)")
	}

	logger := bootstrap.NewLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool := bootstrap.MustConnectDB(ctx, *dbURL, logger)
	cancel()
	defer pool.Close()

	st := postgres.New(pool)
	cutoff := time.Now().UTC().Add(-time.Duration(*retentionDays) * 24 * time.Hour)

	logger.Info("starting event cleanup",
		"retention_days", *retentionDays,
		"cutoff", cutoff.Format(time.RFC3339),
		"batch_size", *batchSize,
	)

	start := time.Now()
	var totalDeleted int64

	for {
		deleted, err := st.DeleteEventsOlderThan(context.Background(), cutoff, *batchSize)
		if err != nil {
			log.Fatalf("delete failed: %v", err)
		}
		totalDeleted += deleted
		if deleted == 0 {
			break
		}
		logger.Info("batch deleted", "rows", deleted, "total", totalDeleted)
	}

	logger.Info("event cleanup complete",
		"total_deleted", totalDeleted,
		"duration", time.Since(start).Round(time.Millisecond),
	)
}

func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var i int
	if _, err := fmt.Sscanf(v, "%d", &i); err != nil {
		return fallback
	}
	return i
}
