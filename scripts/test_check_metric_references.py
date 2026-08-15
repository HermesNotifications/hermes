#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the dashboard/alert metric reference gate.

The case that motivated the gate is `test_catches_the_dashboard_defect_that_shipped`: the
pipeline dashboard queried four metric names no Go file emitted, and neither Grafana nor
Prometheus can tell that apart from a healthy system with no traffic.

The rest pin the two ways a gate like this dies. It can fail loudly on prose — the rules
files discuss metric families as `hermes_messaging_*`, and a pattern that reads the stem
of that glob as a metric name blocks a build over a sentence. Or it can pass everything by
over-normalizing the exporter's suffixes until every name reduces to something emitted.
"""

import os
import tempfile
import unittest

from check_metric_references import (
    check,
    emitted_metrics,
    main,
    referenced_metrics,
    strip_suffixes,
    unread,
)


def write(root, relpath, text):
    path = os.path.join(root, relpath)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)


class TestEmittedMetrics(unittest.TestCase):
    def test_reads_every_instrument_kind(self):
        with tempfile.TemporaryDirectory() as root:
            write(root, "internal/a/metrics.go", """
                var c, _ = meter.Int64Counter("hermes.thing.count")
                var h, _ = meter.Float64Histogram("hermes.thing.duration")
                var g, _ = meter.Int64UpDownCounter("hermes.thing.inflight")
                var o, _ = meter.Int64ObservableGauge("hermes.thing.level")
            """)
            self.assertEqual(
                emitted_metrics(root),
                {
                    "hermes_thing_count",
                    "hermes_thing_duration",
                    "hermes_thing_inflight",
                    "hermes_thing_level",
                },
            )

    def test_walks_cmd_as_well_as_internal(self):
        with tempfile.TemporaryDirectory() as root:
            write(root, "cmd/x/main.go", 'meter.Int64Counter("hermes.x.y")')
            self.assertIn("hermes_x_y", emitted_metrics(root))


class TestStripSuffixes(unittest.TestCase):
    def test_reduces_a_histogram_bucket_to_its_instrument(self):
        # The exporter appends both a unit and a series suffix; the instrument name is
        # only reachable by removing them in sequence.
        self.assertIn(
            "hermes_delivery_provider_duration",
            strip_suffixes("hermes_delivery_provider_duration_seconds_bucket"),
        )

    def test_reduces_a_counter_total(self):
        self.assertIn("hermes_delivery_result", strip_suffixes("hermes_delivery_result_total"))

    def test_keeps_the_original(self):
        self.assertIn("hermes_probe_connected", strip_suffixes("hermes_probe_connected"))


class TestReferencedMetrics(unittest.TestCase):
    def test_reads_dashboards_and_rules(self):
        with tempfile.TemporaryDirectory() as root:
            write(root, "deploy/observability/base/grafana/dashboards/d.json",
                  '{"expr": "sum(rate(hermes_a_b_total[5m]))"}')
            write(root, "deploy/observability/base/prometheus-rules/r.yaml",
                  "expr: hermes_c_d > 1")
            refs = referenced_metrics(root)
            self.assertIn("hermes_a_b_total", refs)
            self.assertIn("hermes_c_d", refs)

    def test_ignores_a_metric_family_glob_in_prose(self):
        # pipeline.rules.yaml's comments refer to families this way. Reading the stem as a
        # metric name fails the build over a comment.
        with tempfile.TemporaryDirectory() as root:
            write(root, "deploy/observability/base/prometheus-rules/r.yaml",
                  "# Hermes' own hermes_messaging_* metrics use stream/consumer\nexpr: up")
            self.assertEqual(referenced_metrics(root), {})

    def test_ignores_recording_rule_names(self):
        # Recording rules define their own colon-separated names, which are not instruments.
        with tempfile.TemporaryDirectory() as root:
            write(root, "deploy/observability/base/prometheus-rules/r.yaml",
                  "expr: hermes:consumer_pending > 1")
            self.assertEqual(referenced_metrics(root), {})


class TestCheck(unittest.TestCase):
    def test_catches_the_dashboard_defect_that_shipped(self):
        emitted = {"hermes_delivery_result", "hermes_notifications_accepted"}
        referenced = {
            "hermes_notifications_sent_total": {"pipeline-overview.json"},
            "hermes_deliveries_failed_total": {"pipeline-overview.json"},
        }
        failures = check(emitted, referenced)
        self.assertEqual(len(failures), 2)
        self.assertIn("hermes_notifications_sent_total", failures[1])
        self.assertIn("pipeline-overview.json", failures[1])

    def test_accepts_exporter_suffixed_names(self):
        emitted = {"hermes_delivery_provider_duration", "hermes_delivery_result"}
        referenced = {
            "hermes_delivery_provider_duration_seconds_bucket": {"d.json"},
            "hermes_delivery_result_total": {"r.yaml"},
        }
        self.assertEqual(check(emitted, referenced), [])


class TestUnread(unittest.TestCase):
    def test_reports_an_instrument_nothing_queries(self):
        # Advisory, not fatal: three of these existed when the gate was written, including
        # hermes_delivery_result, which is the metric that says whether Hermes works.
        emitted = {"hermes_a", "hermes_b"}
        referenced = {"hermes_a_total": {"d.json"}}
        self.assertEqual(unread(emitted, referenced), ["hermes_b"])


class TestMain(unittest.TestCase):
    def test_fails_when_it_finds_no_instruments(self):
        # A gate that reads nothing and reports success is worse than no gate.
        with tempfile.TemporaryDirectory() as root:
            write(root, "deploy/observability/base/grafana/dashboards/d.json",
                  '{"expr": "hermes_a_total"}')
            self.assertEqual(main([f"--root={root}"]), 1)

    def test_fails_when_it_finds_no_references(self):
        with tempfile.TemporaryDirectory() as root:
            write(root, "internal/a/m.go", 'meter.Int64Counter("hermes.a")')
            self.assertEqual(main([f"--root={root}"]), 1)

    def test_passes_a_consistent_tree(self):
        with tempfile.TemporaryDirectory() as root:
            write(root, "internal/a/m.go", 'meter.Int64Counter("hermes.a.b")')
            write(root, "deploy/observability/base/grafana/dashboards/d.json",
                  '{"expr": "sum(rate(hermes_a_b_total[5m]))"}')
            self.assertEqual(main([f"--root={root}"]), 0)


if __name__ == "__main__":
    unittest.main()
