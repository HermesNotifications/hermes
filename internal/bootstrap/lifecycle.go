// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultNATSDrainTimeout is how long a shutting-down service waits for in-flight handlers.
//
// Sized against the delivery workers' 30s handler deadline: long enough for one in-progress
// send to finish and ack, short enough to fit the shutdown budget under a 60s grace period
// (preStop 5 + drain delay 5 + this 30 + HTTP shutdown 15 = 55).
const DefaultNATSDrainTimeout = 30 * time.Second

// DrainNATS returns a shutdown callback that stops consuming and waits for in-flight handlers.
//
// Register it as the *first* callback, and delete the `defer natsClient.Close()` it replaces.
// That defer ran after the HTTP server had already shut down, so the consumer pool went on
// pulling new messages throughout the entire shutdown window — accepting work the process had
// no intention of finishing, all of it redelivered later with its side effects repeated.
func DrainNATS(client *messaging.Client, timeout time.Duration, logger *slog.Logger) func() {
	if timeout <= 0 {
		timeout = DefaultNATSDrainTimeout
	}
	return func() {
		start := time.Now()
		err := client.Drain(timeout)
		elapsed := time.Since(start)
		shutdownDuration.Record(context.Background(), elapsed.Seconds(),
			metric.WithAttributes(attribute.String("phase", "nats_drain")))
		if err != nil {
			// Not fatal, but not silent either: the messages involved are safe (unacked, so
			// redelivered) while their side effects are not (already performed, about to be
			// performed again). This is the counter UngracefulShutdown alerts on.
			ungracefulShutdowns.Add(context.Background(), 1)
			logger.Error("nats drain did not complete cleanly", "error", err, "elapsed", elapsed)
			return
		}
		logger.Info("nats drained", "elapsed", elapsed)
	}
}

// readinessThreshold is how many consecutive failures a dependency must show before the pod
// reports unready.
//
// Two, not one, because probes run every 5s with failureThreshold 1 — chosen so a *draining*
// pod leaves the endpoints on the very next scrape. That same setting would make a single
// dropped packet to Postgres eject a healthy pod, so the debounce lives here instead, where it
// applies to dependency checks and not to the drain flag.
const readinessThreshold = 2

// NewReadiness builds the standard readiness probe: the given dependency checks, debounced.
func NewReadiness(checks ...httputil.Check) *httputil.Readiness {
	return httputil.NewReadiness(readinessThreshold, checks...)
}

// PostgresCheck gates readiness on the database. Correct for every service that cannot answer
// a request without it — which is all of them that hold a pool.
func PostgresCheck(pool *pgxpool.Pool) httputil.Check {
	return httputil.Check{Name: "postgres", Fn: pool.Ping}
}

// NATSCheck gates readiness on the bus, using the locally-known connection status rather than a
// round trip, so a readiness probe never adds load to a bus that is already struggling.
//
// Workers take no inbound traffic, so this does not affect routing for them. It still matters:
// readiness gates *rollout progress*, and refusing to roll forward into a broken bus is exactly
// what should happen.
func NATSCheck(client *messaging.Client) httputil.Check {
	return httputil.Check{
		Name: "nats",
		Fn: func(context.Context) error {
			return client.Healthy()
		},
	}
}

// Deliberately absent: a Redis check.
//
// Redis backs caches that all fall back to Postgres, and an idempotency window whose loss
// degrades to possible duplicate sends. None of that stops a pod serving correct responses. If
// readiness gated on it, one blip on a shared dependency would pull *every* replica out of the
// Service at once — turning a degradation users would barely notice into a total outage. The
// cache result metric is where Redis health belongs.
