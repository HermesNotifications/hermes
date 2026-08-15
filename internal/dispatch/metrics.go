// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"

	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/dispatch")

// routingDrops counts channels routing declined to deliver on, by reason.
//
// A notification that resolves to no channels is not an error — nothing failed, the rules
// simply selected nothing — which is why the log lines for it now sit at Debug. But it is
// also the failure mode most likely to go unnoticed, because it is silent by construction:
// the send succeeds, the API returns 202, and nothing is delivered. The per-occurrence
// warnings that used to cover this were unreadable at volume and absent from any dashboard.
// A rate is the right shape for it — a steady background of no_contact is normal, a step
// change in it is a template or preference regression.
//
// channel is "" for whole-notification drops and a channel name for per-channel ones.
// reason is a closed set fixed by the call sites in dispatch.go. Neither is derived from
// user input; see the forbidden-label table in docs/observability/semantic-conventions.md.
var routingDrops, _ = meter.Int64Counter(
	"hermes.routing.drop",
	metric.WithDescription("Channels not delivered on after routing, by reason."),
	metric.WithUnit("1"),
)

func recordRoutingDrop(ctx context.Context, channel, reason string) {
	routingDrops.Add(ctx, 1, metric.WithAttributes(
		attribute.String("channel", channel),
		attribute.String("reason", reason),
	))
}

// dispatched counts delivery messages that reached the bus, one per notification per
// channel.
//
// This is the middle term of the pipeline's only end-to-end ratio, and it was the
// missing one. hermes.notifications.accepted counts what came in, hermes.delivery.result
// counts what a worker finished, and neither says anything about the fan-out between
// them: a notification accepted once and dispatched to three channels is one accept and
// three deliveries, so accepted and delivered are not comparable without this to relate
// them. It is also what makes routing drops legible — a drop rate means nothing except
// against the dispatches it did not become.
var dispatchedCounter, _ = meter.Int64Counter(
	"hermes.notifications.dispatched",
	metric.WithDescription("Delivery messages published to a channel subject."),
	metric.WithUnit("1"),
)

func recordDispatched(ctx context.Context, channel string) {
	dispatchedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", channel)))
}

// dispatchFailures counts fan-out that failed after routing already decided to deliver.
//
// Distinct from routingDrops on purpose. A drop is the rules working — no contact point,
// no template content, an opt-out — and a healthy system has a steady background of them.
// This is the opposite: routing selected the channel, and the notification was lost
// anyway because the message would not marshal or NATS would not take it. Any nonzero
// rate is a defect, which is what makes it worth separating from a signal whose normal
// value is not zero.
var dispatchFailures, _ = meter.Int64Counter(
	"hermes.dispatch.failures",
	metric.WithDescription("Channels routing selected that could not be published, by reason."),
	metric.WithUnit("1"),
)

func recordDispatchFailure(ctx context.Context, channel, reason string) {
	dispatchFailures.Add(ctx, 1, metric.WithAttributes(
		attribute.String("channel", channel),
		attribute.String("reason", reason),
	))
}

// cacheResults mirrors the instrument of the same name in internal/inbox — same name,
// unit and description, distinguished by the op label — so that one query answers "how
// is the cache doing" across services rather than one query per package.
//
// Templates are the read this service does per notification, and the Redis cache in
// front of them is the reason dispatch does not put a template SELECT on the hot path.
// Whether that is actually happening was unobservable: a cache returning a miss every
// time behaves identically to a warm one, only slower, and the failure is silent because
// the store fallback is correct. The dashboard has had a hit-rate panel pointed at a
// metric nobody ever emitted.
var cacheResults, _ = meter.Int64Counter(
	"hermes.cache.result",
	metric.WithDescription("Cache consultations by operation and outcome."),
	metric.WithUnit("1"),
)

func recordCacheResult(ctx context.Context, op, result string) {
	cacheResults.Add(ctx, 1, metric.WithAttributes(
		attribute.String("op", op),
		attribute.String("result", result),
	))
}
