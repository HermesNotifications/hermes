// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg, logger)
	defer pool.Close()

	// "hermes-send" is not decoration: it selects this service's user, and therefore its
	// subject permissions, in deploy/k8s/base/infra/nats-accounts.conf, and confines the
	// connection's reply inboxes to _INBOX.hermes-send (ADR 0005 phase 3).
	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger,
		messaging.WithCABundle(cfg.NATSCABundlePath),
		messaging.WithIdentity("hermes-send", cfg.NATSNKeySeedPath))
	// ADR 0005 phase 4. Verify, do not declare: cmd/natsprovision owns stream creation, and
	// this service holds no permission to create one. Exits if the streams are not there yet.
	bootstrap.MustEnsureStreams(ctx, natsClient, "hermes-send", logger)
	// Drained as a shutdown callback below rather than deferred here, so buffered publishes are
	// flushed before the process exits.

	redisClient := bootstrap.MustConnectRedis(cfg, logger)
	defer redisClient.Close()

	st := postgres.New(pool)

	srv := send.NewServer(st, natsClient, redisClient, pool, cfg.APIKeyHMACSecret, logger)
	bootstrap.SetupRateLimiting(srv, cfg, redisClient, logger)

	// Publishing is this service's entire job, so an unusable bus makes it unready. Postgres
	// too, for the API key lookup behind the cache.
	readiness := bootstrap.NewReadiness(
		bootstrap.PostgresCheck(pool),
		bootstrap.NATSCheck(natsClient),
	)
	srv.SetReadiness(readiness)

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown:      []func(){bootstrap.DrainNATS(natsClient, cfg.NATSDrainTimeout, logger)},
		})
}
