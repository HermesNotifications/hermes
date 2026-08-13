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
		messaging.WithIdentity("hermes-worker-sms", cfg.NATSNKeySeedPath),
		messaging.WithConsumerStallTimeout(cfg.NATSConsumerStallTimeout))
	// ADR 0005 phase 4. Verify, do not declare — see cmd/natsprovision.
	bootstrap.MustEnsureStreams(ctx, natsClient, "hermes-worker-sms", logger)
	// Drained as a shutdown callback below rather than deferred here — a deferred Close ran
	// after the HTTP server stopped, so the pool kept pulling work it would then abandon.

	provider := delivery.NewWebhookProvider("sms", cfg.SMSWebhookURL)

	worker := delivery.NewWorker(natsClient, provider, "sms", "worker-sms", logger)
	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start worker", "error", err)
		os.Exit(1)
	}

	// The bus only. A failing SMS webhook is a per-message failure that retries and eventually
	// dead-letters; it must not remove delivery capacity.
	readiness := bootstrap.NewReadiness(bootstrap.NATSCheck(natsClient))

	mux := http.NewServeMux()
	// Liveness gates on the consumer still turning. A worker takes no inbound traffic, so
	// readiness cannot express "this pod is not doing its job" — only a restart can act on it, and
	// JetStream redelivers whatever was unacked. See internal/messaging/stall.go.
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler(bootstrap.ConsumerProgressCheck(natsClient)))
	mux.HandleFunc("GET /readyz", readiness.Handler())

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown:      []func(){bootstrap.DrainNATS(natsClient, cfg.NATSDrainTimeout, logger)},
		})
}
