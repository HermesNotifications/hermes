// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/dispatchbench"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/hermes-notifications/hermes/internal/store/cached"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

// envOr returns the value of environment variable k, or d if unset.
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// must aborts the program if err is non-nil.
func must(err error, what string) {
	if err != nil {
		log.Fatalf("%s: %v", what, err)
	}
}

// seedBench inserts a bench tenant and nUsers users (with emails so channel
// resolution is realistic), warms the Redis tenant cache, and returns the users'
// external IDs (the send message addresses users by external ID).
func seedBench(ctx context.Context, pool *pgxpool.Pool, pgStore *postgres.Store, redisClient *cache.Client, tenant string, nUsers int) []string {
	_, err := pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1,$2) ON CONFLICT DO NOTHING", tenant, "dispatchbench")
	must(err, "insert bench tenant")

	externalIDs := make([]string, 0, nUsers)
	for i := 0; i < nUsers; i++ {
		u, err := pgStore.EnsureUser(ctx, tenant, fmt.Sprintf("u%d", i))
		must(err, "ensure bench user")
		email := fmt.Sprintf("u%d@bench.local", i)
		if _, err := pgStore.UpdateUserContacts(ctx, u.ID, &email, nil); err != nil {
			must(err, "set bench user email")
		}
		externalIDs = append(externalIDs, u.ExternalID)
	}

	// Warm the Redis tenant cache so dispatch's EnsureTenant is a cache hit.
	tenants := cached.NewTenantRepository(pgStore, redisClient)
	if _, err := tenants.EnsureTenant(ctx, tenant); err != nil {
		must(err, "warm tenant cache")
	}
	return externalIDs
}

// storeForBackend returns the NotificationRepository for the given backend, or a
// nil interface (skip) when the backend is unavailable.
func storeForBackend(ctx context.Context, backend string, pgStore *postgres.Store, dynamoEP, dynamoRgn string, logger *slog.Logger) store.NotificationRepository {
	switch backend {
	case "postgres":
		return pgStore
	case "dynamo":
		if dynamoEP == "" {
			return nil
		}
		client, err := dynamo.NewClient(ctx, dynamoEP, dynamoRgn)
		if err != nil {
			logger.Warn("dynamo connect failed; skipping dynamo cells", "error", err)
			return nil
		}
		if err := client.EnsureTables(ctx); err != nil {
			logger.Warn("dynamo ensure tables failed; skipping dynamo cells", "error", err)
			return nil
		}
		return dynamo.NewNotificationStore(client, dynamo.NewEventStore(client, pgStore))
	default:
		logger.Warn("unknown backend; skipping", "backend", backend)
		return nil
	}
}

// newRunner wires a dispatchbench.Runner with the publish/dispatch/reset closures
// for one backend's notification repository.
func newRunner(
	js jetstream.JetStream,
	natsURL string,
	n int,
	tenant string,
	userIDs []string,
	notifRepo store.NotificationRepository,
	pgStore *postgres.Store,
	redisClient *cache.Client,
	admin *messaging.Client,
	pool *pgxpool.Pool,
	logger *slog.Logger,
) *dispatchbench.Runner {
	publish := func(ctx context.Context, n int) error {
		for i := 0; i < n; i++ {
			msg := &hermenats.SendMessage{
				NotificationID: id.Notification.New(),
				TenantID:       tenant,
				ExternalUserID: userIDs[i%len(userIDs)],
				Content:        &hermenats.MessageContent{Title: "bench", Body: "bench"},
				Channels:       []string{"inbox"},
			}
			data, err := msg.Marshal()
			if err != nil {
				return err
			}
			if err := admin.Publish(ctx, "notification.send", data); err != nil {
				return err
			}
		}
		return nil
	}

	dispatchFactory := func(workers, prefetch int) (func(), error) {
		client, err := messaging.Connect(natsURL)
		if err != nil {
			return nil, err
		}
		templateResolver := dispatch.NewTemplateResolver(pgStore, redisClient)
		channelResolver := dispatch.NewChannelResolver(pgStore, redisClient)
		tenants := cached.NewTenantRepository(pgStore, redisClient)
		d := dispatch.NewDispatch(client, notifRepo, pgStore, tenants, templateResolver, channelResolver, logger)
		if err := d.Start(workers, prefetch); err != nil {
			client.Close()
			return nil, err
		}
		return func() { client.Close() }, nil
	}

	reset := func(ctx context.Context) error {
		for _, s := range []string{"NOTIFICATIONS", "DELIVERY", "EVENTS"} {
			if st, err := js.Stream(ctx, s); err == nil {
				if err := st.Purge(ctx); err != nil {
					return err
				}
			}
		}
		// Best-effort: consumer may not exist yet on the first repetition.
		_ = js.DeleteConsumer(ctx, "NOTIFICATIONS", "dispatch")
		if _, err := pool.Exec(ctx,
			"DELETE FROM notification_events WHERE notification_id IN (SELECT id FROM notifications WHERE tenant_id=$1)",
			tenant); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, "DELETE FROM notifications WHERE tenant_id=$1", tenant); err != nil {
			return err
		}
		return nil
	}

	return &dispatchbench.Runner{
		JS:       js,
		Stream:   "NOTIFICATIONS",
		Consumer: "dispatch",
		N:        n,
		Publish:  publish,
		Dispatch: dispatchFactory,
		Reset:    reset,
		Poll:     50 * time.Millisecond,
	}
}

// writeOutputs writes the CSV and markdown reports.
func writeOutputs(csvPath, mdPath string, results []dispatchbench.Result) {
	f, err := os.Create(csvPath)
	must(err, "create csv")
	defer f.Close()
	must(dispatchbench.WriteCSV(f, results), "write csv")

	must(os.WriteFile(mdPath, []byte(dispatchbench.Markdown(results)), 0o644), "write markdown")
}
