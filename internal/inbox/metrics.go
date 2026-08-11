// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package inbox

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermes-notifications/hermes/internal/inbox")

// cacheResults records the outcome of every cache consultation.
//
// Two jobs. The obvious one is a hit ratio: this read path only stops touching the database if
// the cache is actually being hit, and before this change nothing would have revealed that it
// never was.
//
// The load-bearing one is `result=error`. Redis deliberately does not gate readiness — every
// read it serves falls back to Postgres, so a pod with a sick Redis still answers correctly,
// and marking it unready would pull every replica out of the Service over a fault none of them
// needed to stop serving for. The consequence is that Redis can be failing continuously with
// every probe green. This counter is the only place that shows up.
var cacheResults, _ = meter.Int64Counter(
	"hermes.cache.result",
	metric.WithDescription("Cache consultations by operation and outcome."),
	metric.WithUnit("1"),
)

// recordCacheResult reports one cache consultation. Both labels are closed sets, so the
// cardinality is bounded — see docs/observability/semantic-conventions.md.
func recordCacheResult(ctx context.Context, op, result string) {
	cacheResults.Add(ctx, 1, metric.WithAttributes(
		attribute.String("op", op),
		attribute.String("result", result),
	))
}
