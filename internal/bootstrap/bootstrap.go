// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/observability"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewLogger creates a structured JSON logger writing to stdout.
// The handler is wrapped with observability.TraceHandler so every record
// picks up trace_id/span_id when the context carries an active span.
func NewLogger() *slog.Logger {
	return slog.New(observability.WrapJSONHandler(slog.NewJSONHandler(os.Stdout, nil)))
}

// MustConnectDB connects to Postgres with bounds taken from cfg, or exits the process.
//
// The bounds are not optional detail. pgx sizes its pool from the node's core count by default,
// so before this the same image opened anywhere from 4 to 16 connections depending on where the
// scheduler placed it — which made the cluster-wide connection total unknowable, and
// unknowable is how you find max_connections by hitting it.
func MustConnectDB(ctx context.Context, cfg config.Config, logger *slog.Logger) *pgxpool.Pool {
	pool, err := database.NewPoolWithConfig(ctx, cfg.DatabaseURL, database.PoolConfig{
		MaxConns:        int32(cfg.DatabaseMaxConns),
		MinConns:        int32(cfg.DatabaseMinConns),
		MaxConnLifetime: cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime: cfg.DatabaseMaxConnIdleTime,
	})
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	logger.Info("database connected", "max_conns", pool.Config().MaxConns)
	return pool
}

// MustConnectNATS connects to NATS or exits the process.
//
// Callers pass messaging.WithCABundle(cfg.NATSCABundlePath) so transport security comes
// from configuration rather than being hardcoded here (ADR 0005 phase 2). Exiting is the
// fail-closed behaviour: a service that cannot verify the bus does not run.
func MustConnectNATS(url string, logger *slog.Logger, opts ...messaging.Option) *messaging.Client {
	client, err := messaging.Connect(url, opts...)
	if err != nil {
		logger.Error("nats connection failed", "error", err)
		os.Exit(1)
	}
	return client
}

// MustEnsureStreams verifies that the streams the named service depends on exist, or exits the
// process.
//
// ADR 0005 phase 4. This replaced MustSetupStreams in every service. Declaring streams needs
// STREAM.CREATE and STREAM.UPDATE, and granting that to all six services let any one of them
// rewrite the configuration of streams it never touched; cmd/natsprovision now holds those
// rights alone. Exiting preserves the property that made self-declaration attractive: a service
// never runs against a bus that is not ready, it just cannot repair one either.
func MustEnsureStreams(ctx context.Context, client *messaging.Client, service string, logger *slog.Logger) {
	if err := client.EnsureStreams(ctx, service); err != nil {
		logger.Error("nats streams are not ready", "service", service, "error", err)
		os.Exit(1)
	}
}

// MustConnectRedis connects to Redis with bounds taken from cfg, or exits the process.
func MustConnectRedis(cfg config.Config, logger *slog.Logger) *cache.Client {
	client, err := cache.ConnectWithOptions(cfg.RedisURL, cache.Options{
		PoolSize: cfg.RedisPoolSize,
		Timeout:  cfg.RedisTimeout,
	})
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	return client
}

// MustConnectDynamo creates a DynamoDB client (or ExtendDB client when endpoint
// is non-empty) and ensures the required tables exist, or exits the process.
// retentionDays sets the event TTL window (pass cfg.EventRetentionDays; 0 → default 90).
// It is safe to call even when the DynamoDB path is not yet wired into service
// handlers — table creation is idempotent.
func MustConnectDynamo(ctx context.Context, endpoint, region string, retentionDays int, logger *slog.Logger) *dynamo.Client {
	client, err := dynamo.NewClient(ctx, endpoint, region)
	if err != nil {
		logger.Error("dynamo client creation failed", "error", err)
		os.Exit(1)
	}
	if retentionDays > 0 {
		client.RetentionDays = retentionDays
	}
	if err := client.EnsureTables(ctx); err != nil {
		logger.Error("dynamo ensure tables failed", "error", err)
		os.Exit(1)
	}
	mode := "AWS DynamoDB"
	if endpoint != "" {
		mode = "ExtendDB @ " + endpoint
	}
	logger.Info("dynamo connected", "mode", mode, "event_retention_days", client.RetentionDays)
	return client
}
