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
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/email"
	"github.com/hermes-notifications/hermes/internal/httputil"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger)
	bootstrap.MustSetupStreams(ctx, natsClient, logger)
	defer natsClient.Close()

	emailCfg := email.Config{
		Provider:     cfg.Email.Provider,
		From:         cfg.Email.From,
		SMTPHost:     cfg.Email.SMTPHost,
		SMTPPort:     cfg.Email.SMTPPort,
		SMTPUsername: cfg.Email.SMTPUsername,
		SMTPPassword: cfg.Email.SMTPPassword,
		SESRegion:    cfg.Email.SESRegion,
		LayoutPath:   cfg.Email.LayoutPath,
	}

	emailProvider, err := email.NewProvider(emailCfg)
	if err != nil {
		logger.Error("create email provider", "error", err)
		os.Exit(1)
	}

	layout := email.MustLoadLayout(cfg.Email.LayoutPath, logger)
	adapter := email.NewDeliveryAdapter(emailProvider, cfg.Email.From, layout)

	worker := delivery.NewWorker(natsClient, adapter, "email", "worker-email", logger)
	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start worker", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	mux.HandleFunc("GET /readyz", httputil.ReadyzHandler())

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger)
}
