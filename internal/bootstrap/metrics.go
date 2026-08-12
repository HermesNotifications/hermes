// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package bootstrap

import (
	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/bootstrap")

var (
	// shutdownDuration measures each phase of the shutdown sequence.
	//
	// The budget it belongs to is split across a manifest field and three environment
	// variables, and scripts/check_shutdown_budget.py can only check the declared numbers add
	// up. Whether the *actual* drain fits inside the grace period is an empirical question, and
	// this is the only thing that answers it. Compare the sum against
	// terminationGracePeriodSeconds; if it is creeping up, the pod is heading for a SIGKILL
	// mid-drain.
	shutdownDuration, _ = meter.Float64Histogram(
		"hermes.shutdown.duration",
		metric.WithDescription("Time spent in one phase of the graceful shutdown sequence."),
		metric.WithUnit("s"),
	)

	// ungracefulShutdowns counts drains that timed out with handlers still running.
	//
	// Should be zero. A non-zero value means messages were abandoned mid-handler: they are not
	// lost — unacked, so JetStream redelivers them — but their side effects will be repeated,
	// which is a duplicate email or a duplicate push per occurrence.
	ungracefulShutdowns, _ = meter.Int64Counter(
		"hermes.shutdown.ungraceful",
		metric.WithDescription("Shutdowns where the drain timed out with work still in flight."),
		metric.WithUnit("1"),
	)
)
