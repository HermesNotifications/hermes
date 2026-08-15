// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package eventwriter

import (
	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/eventwriter")

// This package is the end of the pipeline — every event any service emits lands here and
// becomes a row, and the status ladder every notification is read through is maintained
// from these writes. It had no metrics at all. The only evidence it was running was a
// per-flush log line, and that line has since been demoted to Debug for the reason
// semantic-conventions.md gives: a record that fires twice a second per replica is an
// expensive way to store a number. The counter it says to add first is this file.
//
// The failure this closes is specific. InsertEvents failing is logged and returned from
// flush, which drops the batch — the messages were already acked when they entered the
// batcher, so nothing redelivers them. Events vanish, statuses stop advancing, and the
// only surviving signal was an Error log in a service nobody watches at Error.
var (
	// flushDuration times the database write, and its _count is the flush rate. The
	// batcher fires on 100 items or 500ms, so a flush rate pinned at 2/s per replica
	// means every flush is a timer flush and the pipeline is idle; a rate far above it
	// means batches are filling, which is the healthy shape under load.
	flushDuration, _ = meter.Float64Histogram(
		"hermes.eventwriter.flush.duration",
		metric.WithDescription("Time to write one batch of events and status updates."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)

	// batchSize is the tuning signal for the batcher's two constants. Against the
	// configured maximum it says which of the two is binding: a distribution hugging 100
	// means size is the trigger and the interval is irrelevant, one hugging 1 means the
	// reverse and the batching is buying nothing.
	batchSize, _ = meter.Int64Histogram(
		"hermes.eventwriter.batch.size",
		metric.WithDescription("Events per flush."),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(1, 2, 5, 10, 25, 50, 75, 100),
	)

	// eventsWritten is the far end of the pipeline's throughput chain — accepted in send,
	// dispatched in dispatch, delivered in delivery, recorded here.
	eventsWritten, _ = meter.Int64Counter(
		"hermes.eventwriter.events",
		metric.WithDescription("Events successfully written to the database."),
		metric.WithUnit("1"),
	)

	// eventsDropped is the one that has to page. stage distinguishes the two writes:
	// `insert` loses the events outright, `status` keeps the events but leaves
	// notifications stuck at a stale status, which surfaces to users as a notification
	// that never turns delivered. Neither is recoverable by retry — the messages are long
	// acked — so any nonzero value is permanent data loss.
	eventsDropped, _ = meter.Int64Counter(
		"hermes.eventwriter.dropped",
		metric.WithDescription("Events lost because a batch write failed, by stage."),
		metric.WithUnit("1"),
	)
)
