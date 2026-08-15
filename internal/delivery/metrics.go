// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/delivery")

// deliveryResults counts terminal delivery outcomes per channel.
//
// This exists because the log line it replaces was load-bearing by accident. "delivery
// succeeded" at Info was, before this, the only signal in the whole delivery path that
// anything was working — the package had no metrics at all — so the only way to answer
// "are we delivering?" was to count log records. That is an expensive way to store a
// number, and it stops working exactly when you need it, because log volume is the first
// thing shed under load.
//
// Counted once per notification per channel, at the terminal outcome only: intermediate
// retry failures are not counted, so success + failed equals notifications that reached a
// decision, and a flaky provider that eventually delivers reads as one success rather than
// several failures and a success.
//
// All three labels are closed sets — channel is the registry's fixed list, provider is
// the backend configured for it, outcome is success|failed. No notification_id or
// user_id: see the forbidden-label table in docs/observability/semantic-conventions.md.
//
// provider is here because a channel is not a single dependency. "email is failing" is
// a different incident, and a different fix, depending on whether every provider behind
// it is failing or one is; without the label the ratio silently averages them.
var deliveryResults, _ = meter.Int64Counter(
	"hermes.delivery.result",
	metric.WithDescription("Terminal delivery outcomes by channel and outcome."),
	metric.WithUnit("1"),
)

// providerDuration times the call out to the provider, which is the only part of a
// delivery Hermes does not control and the first thing to suspect when the pipeline is
// slow.
//
// hermes.messaging.handler.duration already covers the handler this sits inside, but it
// is labelled by consumer, so a slow SendGrid and a slow database look the same in it.
// Splitting the provider call out is what separates "our problem" from "their problem",
// and it is the number to compare against AckWait — a provider whose p99 approaches the
// ack deadline is the specific cause of the redelivery storm the messaging package's
// comments warn about.
//
// provider is the registry's own name for the backend, a fixed set decided at
// configuration time, not a hostname or an endpoint.
//
// Recorded on failures as well as successes: a provider that fails fast and one that
// fails by timing out call for different fixes, and a histogram of successes only would
// hide the difference exactly when it matters.
var providerDuration, _ = meter.Float64Histogram(
	"hermes.delivery.provider.duration",
	metric.WithDescription("Time spent in the provider call for one delivery attempt."),
	metric.WithUnit("s"),
	metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30),
)

// outcomeOf keeps the outcome label to the same two words the result counter uses, so a
// query can move between the two instruments without translating.
func outcomeOf(err error) string {
	if err != nil {
		return "failed"
	}
	return "success"
}
