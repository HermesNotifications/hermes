// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

import (
	"go.opentelemetry.io/otel/metric"
)

// Instruments for the consumer path. Named per docs/observability/semantic-conventions.md:
// dotted, no verbs, no `_total` suffix, and only bounded label values.
//
// These exist because the two settings this package now depends on — AckWait and the drain —
// are otherwise unfalsifiable in production. A redelivery looks identical to a retry in the
// logs, so without a metric that separates them the only evidence of an AckWait set too low is
// a customer reporting a duplicate email.
var (
	// handlerDuration is the metric that says whether AckWait is right. Alert when its p99
	// approaches the configured AckWait: past that point JetStream is redelivering messages
	// whose handlers are still running, and every side effect happens twice.
	handlerDuration, _ = meter.Float64Histogram(
		"hermes.messaging.handler.duration",
		metric.WithDescription("Time spent in a message handler, by outcome."),
		metric.WithUnit("s"),
	)

	// redeliveries counts messages arriving with NumDelivered > 1. A spike here *without* a
	// matching rise in handler failures is the signature of an AckWait problem rather than a
	// downstream one — the distinction that is impossible to make from logs alone.
	redeliveries, _ = meter.Int64Counter(
		"hermes.messaging.redeliveries",
		metric.WithDescription("Messages delivered more than once, whether from a nack or an expired ack deadline."),
		metric.WithUnit("1"),
	)

	// inflight is what Drain waits on, exposed so a drain that is not converging is visible
	// while it happens rather than only afterwards via the timeout error.
	inflightGauge, _ = meter.Int64UpDownCounter(
		"hermes.messaging.inflight",
		metric.WithDescription("Messages handed to a worker pool and not yet finished."),
		metric.WithUnit("1"),
	)
)
