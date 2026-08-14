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
	"github.com/hermesnotifications/hermes/internal/centrifugo"
	"github.com/hermesnotifications/hermes/internal/config"
	"github.com/hermesnotifications/hermes/internal/delivery"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/messaging"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger,
		messaging.WithCABundle(cfg.NATSCABundlePath),
		messaging.WithIdentity("hermes-worker-inbox", cfg.NATSNKeySeedPath),
		messaging.WithConsumerStallTimeout(cfg.NATSConsumerStallTimeout))
	// ADR 0005 phase 4. Verify, do not declare — see cmd/natsprovision.
	bootstrap.MustEnsureStreams(ctx, natsClient, "hermes-worker-inbox", logger)
	// No `defer natsClient.Close()`: it ran after the HTTP server had already stopped, so the
	// consumer pool kept pulling new work for the whole shutdown window. Draining is registered
	// as a shutdown callback below instead.

	redisClient := bootstrap.MustConnectRedis(cfg, logger)
	defer redisClient.Close()

	centrifugoClient := centrifugo.NewClient(cfg.CentrifugoAPIURL, cfg.CentrifugoAPIKey)

	provider := delivery.NewInboxProvider(centrifugoClient, redisClient, logger)

	worker := delivery.NewWorker(natsClient, provider, "inbox", "worker-inbox", logger)
	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start worker", "error", err)
		os.Exit(1)
	}

	// The bus, not Redis: a worker that cannot consume has nothing to do, whereas one whose
	// cache is down still delivers (it just cannot attach an unread count).
	readiness := bootstrap.NewReadiness(bootstrap.NATSCheck(natsClient))

	mux := http.NewServeMux()
	// Liveness gates on the consumer still turning. A worker takes no inbound traffic, so
	// readiness cannot express "this pod is not doing its job" — only a restart can act on it, and
	// JetStream redelivers whatever was unacked. See internal/messaging/stall.go.
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler(bootstrap.ConsumerProgressCheck(natsClient)))
	mux.HandleFunc("GET /readyz", readiness.Handler())

	// pprof on its own port when HERMES_DEBUG_PORT is set; a no-op otherwise.
	bootstrap.StartDebugServer(cfg.DebugPort, cfg.BlockProfileRate, cfg.MutexProfileFraction, logger)

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown:      []func(){bootstrap.DrainNATS(natsClient, cfg.NATSDrainTimeout, logger)},
		})
}
