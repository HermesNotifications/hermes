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
// Both labels are closed sets — channel is the registry's fixed list, outcome is
// success|failed. No notification_id or user_id: see the forbidden-label table in
// docs/observability/semantic-conventions.md.
var deliveryResults, _ = meter.Int64Counter(
	"hermes.delivery.result",
	metric.WithDescription("Terminal delivery outcomes by channel and outcome."),
	metric.WithUnit("1"),
)
