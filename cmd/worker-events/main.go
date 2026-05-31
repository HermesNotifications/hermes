// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg.DatabaseURL, logger)
	defer pool.Close()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger)
	bootstrap.MustSetupStreams(ctx, natsClient, logger)
	defer natsClient.Close()

	pgStore := postgres.New(pool)

	var eventRepo store.EventRepository = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, logger)
		// Phase 2: EventStore delegates status updates to NotificationStore (DynamoDB)
		// instead of pgStore (Postgres), completing the migration of the notifications table.
		evStore := dynamo.NewEventStore(dynamoClient, pgStore) // placeholder; updated below
		notifStore := dynamo.NewNotificationStore(dynamoClient, evStore)
		eventRepo = dynamo.NewEventStore(dynamoClient, notifStore)
	}

	w := eventwriter.New(natsClient, eventRepo, logger)

	if err := w.Start(context.Background()); err != nil {
		logger.Error("start event writer", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	mux.HandleFunc("GET /readyz", httputil.ReadyzHandler(pool.Ping))

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger, w.Stop)
}
