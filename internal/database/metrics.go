// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package database

import (
	"context"

	"github.com/hermesnotifications/hermes/internal/observability"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func stateAttr(state string) attribute.KeyValue { return attribute.String("state", state) }

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/database")

// instrumentPool publishes pool saturation as observable gauges.
//
// otelpgx traces individual queries but says nothing about the pool itself, which is the thing
// that actually falls over: once every connection is checked out, requests queue on acquire and
// the symptom is latency with no slow query to blame it on.
// docs/observability/runbooks/db-pool-saturated.md has documented that incident since before
// there was any metric that could detect it — and before HERMES_DATABASE_MAX_CONNS existed to
// fix it.
//
// Errors are ignored deliberately: failing to register a gauge must not stop a service from
// starting.
func instrumentPool(pool *pgxpool.Pool) {
	connections, err := meter.Int64ObservableGauge(
		"hermes.db.pool.connections",
		metric.WithDescription("Pool connections by state."),
		metric.WithUnit("1"),
	)
	if err != nil {
		return
	}
	maxConns, err := meter.Int64ObservableGauge(
		"hermes.db.pool.max",
		metric.WithDescription("Configured maximum pool size, so saturation can be expressed as a ratio."),
		metric.WithUnit("1"),
	)
	if err != nil {
		return
	}
	// The leading indicator. A non-zero rate here means requests are waiting for a connection,
	// which is visible as latency long before anything errors.
	waits, err := meter.Int64ObservableCounter(
		"hermes.db.pool.acquire.waits",
		metric.WithDescription("Acquires that found the pool empty and had to wait."),
		metric.WithUnit("1"),
	)
	if err != nil {
		return
	}

	_, _ = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			stat := pool.Stat()
			o.ObserveInt64(connections, int64(stat.AcquiredConns()), metric.WithAttributes(stateAttr("acquired")))
			o.ObserveInt64(connections, int64(stat.IdleConns()), metric.WithAttributes(stateAttr("idle")))
			o.ObserveInt64(connections, int64(stat.ConstructingConns()), metric.WithAttributes(stateAttr("constructing")))
			o.ObserveInt64(maxConns, int64(stat.MaxConns()))
			o.ObserveInt64(waits, stat.EmptyAcquireCount())
			return nil
		},
		connections, maxConns, waits,
	)
}
