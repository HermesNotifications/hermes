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
	"github.com/hermesnotifications/hermes/internal/dispatch"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/store"
	"github.com/hermesnotifications/hermes/internal/store/cached"
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
		messaging.WithIdentity("hermes-dispatch", cfg.NATSNKeySeedPath))
	// ADR 0005 phase 4. Verify, do not declare — see cmd/natsprovision.
	bootstrap.MustEnsureStreams(ctx, natsClient, "hermes-dispatch", logger)
	// Drained as a shutdown callback below rather than deferred here — a deferred Close ran
	// after the HTTP server stopped, so the pool kept pulling work it would then abandon.

	redisClient := bootstrap.MustConnectRedis(cfg, logger)
	defer redisClient.Close()

	pgStore := postgres.New(pool)
	organizations := cached.NewOrganizationRepository(pgStore, redisClient)
	templateResolver := dispatch.NewTemplateResolver(pgStore, redisClient)
	channelResolver := dispatch.NewChannelResolver(pgStore, redisClient)

	var notifRepo store.NotificationRepository = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, cfg.EventRetentionDays, logger)
		// EventStore is created here only to satisfy NotificationStore's events field;
		// dispatch doesn't insert events — it only creates/updates notification records.
		evStore := dynamo.NewEventStore(dynamoClient, pgStore)
		notifRepo = dynamo.NewNotificationStore(dynamoClient, evStore)
	}

	d := dispatch.NewDispatch(natsClient, notifRepo, pgStore, organizations, templateResolver, channelResolver, logger,
		dispatch.WithIdentityCache(cfg.DispatchIdentityCacheSize))

	// Cap workers at the DB pool size: each worker holds at most one Postgres
	// connection while processing, so more workers than connections only adds
	// contention. A guardrail, not a tuning model — see dispatch.ClampWorkersToPool.
	workers := cfg.DispatchConcurrency
	dbMaxConns := int(pool.Config().MaxConns)
	if eff, clamped := dispatch.ClampWorkersToPool(workers, dbMaxConns); clamped {
		logger.Warn("HERMES_DISPATCH_CONCURRENCY exceeds the database pool size; clamping to pool size",
			"requested", workers, "db_max_conns", dbMaxConns, "effective", eff,
			"hint", "raise pool_max_conns in HERMES_DATABASE_URL to use more workers")
		workers = eff
	}

	if err := d.Start(workers, cfg.DispatchPrefetch); err != nil {
		logger.Error("dispatch start failed", "error", err)
		os.Exit(1)
	}

	// Both, because dispatch cannot do its job without either: it persists the notification to
	// Postgres and fans out over the bus. Redis is excluded — template and channel lookups fall
	// back to the database.
	readiness := bootstrap.NewReadiness(
		bootstrap.PostgresCheck(pool),
		bootstrap.NATSCheck(natsClient),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	mux.HandleFunc("GET /readyz", readiness.Handler())

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown:      []func(){bootstrap.DrainNATS(natsClient, cfg.NATSDrainTimeout, logger)},
		})
}
