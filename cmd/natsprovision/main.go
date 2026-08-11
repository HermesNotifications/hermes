// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Command natsprovision declares the JetStream streams the pipeline runs on, then exits.
//
// ADR 0005 phase 4. Until phase 4 every service called SetupStreams at boot, which meant every
// service's NKey user needed STREAM.CREATE and STREAM.UPDATE on all four streams — including
// streams it neither published to nor consumed. That is the over-grant phase 3 named and
// accepted. This command exists so exactly one identity holds those rights.
//
// It is the messaging counterpart of cmd/migrate: streams are schema, and the repository
// already provisions schema with a Job that runs before the services. Services now verify
// rather than declare (messaging.EnsureStreams) and refuse to start if a stream is missing, so
// running out of order is a crash-loop that converges, not a silent misconfiguration.
//
// Idempotent by construction — SetupStreams is CreateOrUpdate — so re-running it on every
// deploy is the intended usage.
package main

import (
	"context"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The same transport security every service uses, and the provisioner's own credential —
	// deliberately a different NKey from any service's, so this binary is the only thing on
	// the bus that can shape a stream.
	client := bootstrap.MustConnectNATS(cfg.NATSUrl, logger,
		messaging.WithCABundle(cfg.NATSCABundlePath),
		messaging.WithIdentity(messaging.ProvisionerService, cfg.NATSNKeySeedPath))
	defer client.Close()

	if err := client.SetupStreams(ctx); err != nil {
		// Exiting non-zero is what makes the Job fail and retry rather than reporting success
		// on a bus with no streams, which every service would then refuse to start against.
		logger.Error("nats stream provisioning failed", "error", err)
		os.Exit(1)
	}
	logger.Info("nats streams provisioned", "streams", messaging.StreamNames())
}
