// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

	// workersLimit is the denominator inflight was missing. On its own inflight says "6
	// messages in flight", which means nothing without the pool size next to it; together
	// they are pool saturation, and saturation is the signal that names its own fix.
	//
	// Worth the extra series because pool size is the largest throughput lever in the
	// system and it is invisible from the outside. cmd/dispatchbench measured 2,100 msg/s
	// at the default 8 workers and 7,907 at 64, against the same storage — and the way that
	// was found was a benchmark, because production emitted nothing that said "every worker
	// is busy and the queue is growing". inflight/limit at 1.0 with lag rising is that
	// statement, and it points at HERMES_DISPATCH_CONCURRENCY rather than at the disk.
	//
	// Set once per subscription rather than observed: the pool is fixed at Subscribe and
	// only ever leaves by the process exiting.
	workersLimit, _ = meter.Int64UpDownCounter(
		"hermes.messaging.workers.limit",
		metric.WithDescription("Size of the worker pool processing a subscription."),
		metric.WithUnit("1"),
	)
)
