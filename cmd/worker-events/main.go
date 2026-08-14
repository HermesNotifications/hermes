// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/hermesnotifications/hermes/internal/bootstrap"
	"github.com/hermesnotifications/hermes/internal/config"
	"github.com/hermesnotifications/hermes/internal/eventwriter"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/store"
	"github.com/hermesnotifications/hermes/internal/store/dynamo"
	"github.com/hermesnotifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg, logger)
	defer pool.Close()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger,
		messaging.WithCABundle(cfg.NATSCABundlePath),
		messaging.WithIdentity("hermes-worker-events", cfg.NATSNKeySeedPath),
		messaging.WithConsumerStallTimeout(cfg.NATSConsumerStallTimeout))
	// ADR 0005 phase 4. Verify, do not declare — see cmd/natsprovision.
	bootstrap.MustEnsureStreams(ctx, natsClient, "hermes-worker-events", logger)
	// Drained as a shutdown callback below rather than deferred here — a deferred Close ran
	// after the HTTP server stopped, so the pool kept pulling work it would then abandon.

	pgStore := postgres.New(pool)

	var eventRepo store.EventRepository = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, cfg.EventRetentionDays, logger)
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

	readiness := bootstrap.NewReadiness(
		bootstrap.PostgresCheck(pool),
		bootstrap.NATSCheck(natsClient),
	)

	mux := http.NewServeMux()
	// Liveness gates on the consumer still turning. The event writer takes no inbound traffic, so
	// readiness cannot express "this pod is not doing its job" — only a restart can act on it, and
	// JetStream redelivers whatever was unacked. See internal/messaging/stall.go.
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler(bootstrap.ConsumerProgressCheck(natsClient)))
	mux.HandleFunc("GET /readyz", readiness.Handler())

	// Order is load-bearing: drain the consumers first so every in-flight handler has finished
	// adding to the batch, and only then flush it. Flushing first would write a batch that the
	// still-running handlers are appending to, and those late events would be lost.
	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown: []func(){
				bootstrap.DrainNATS(natsClient, cfg.NATSDrainTimeout, logger),
				w.Stop,
			},
		})
}
