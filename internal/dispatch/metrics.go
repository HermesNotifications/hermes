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
