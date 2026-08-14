// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/hermesnotifications/hermes/internal/bootstrap"
	"github.com/hermesnotifications/hermes/internal/config"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/prober"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	if cfg.ProberAPIKey == "" {
		logger.Error("HERMES_PROBER_API_KEY is required")
		os.Exit(1)
	}

	p := prober.New(prober.Config{
		AdminURL:       cfg.ProberAdminURL,
		SendURL:        cfg.ProberSendURL,
		CentrifugoURL:  cfg.ProberCentrifugoURL,
		APIKey:         cfg.ProberAPIKey,
		OrganizationID: cfg.ProberOrganizationID,
		UserID:         cfg.ProberUserID,
		Interval:       cfg.ProberInterval,
		Timeout:        cfg.ProberTimeout,
	}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run in the background so the HTTP server owns the shutdown sequence, the same shape every
	// other service uses. A prober that cannot reach its dependencies at boot exits rather than
	// idling: it holds no traffic, so there is nothing to degrade gracefully, and a pod that
	// silently probes nothing is precisely the failure this service exists to prevent.
	go func() {
		if err := p.Run(ctx); err != nil {
			logger.Error("prober stopped", "error", err)
			os.Exit(1)
		}
	}()

	// Readiness has nothing to check that liveness does not. There is no inbound traffic to be
	// removed from, so the useful signal is the probe result stream itself -- see the
	// HermesProbeLoss alert rather than expecting a probe to fail its own health check.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	mux.HandleFunc("GET /readyz", httputil.HealthzHandler())

	// pprof on its own port when HERMES_DEBUG_PORT is set; a no-op otherwise.
	bootstrap.StartDebugServer(cfg.DebugPort, cfg.BlockProfileRate, cfg.MutexProfileFraction, logger)

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger,
		bootstrap.ServeOptions{
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
			OnShutdown:      []func(){cancel},
		})
}
