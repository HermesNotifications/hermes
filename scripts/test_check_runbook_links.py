#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the alert/runbook pairing gate.

The rules files have claimed "CI check enforces this pairing" since they were written, and
nothing did. These pin the check that now makes the claim true — including that it fails
loudly when it reads no alerts, which is the way a gate like this dies quietly.
"""

import os
import tempfile
import unittest

from check_runbook_links import check, iter_alerts, main, orphans, runbook_slug

BASE_URL = "https://github.com/darylrobbins/hermes/blob/main/docs/observability/runbooks/"


def rule_doc(*alerts):
    """A PrometheusRule wrapping (name, annotations) pairs."""
    return {
        "kind": "PrometheusRule",
        "spec": {"groups": [{"name": "g", "rules": [
            {"alert": name, "annotations": annotations} for name, annotations in alerts
        ]}]},
    }


class TestIterAlerts(unittest.TestCase):
    def test_reads_every_alert_across_groups(self):
        doc = {
            "kind": "PrometheusRule",
            "spec": {"groups": [
                {"name": "a", "rules": [{"alert": "One", "annotations": {}}]},
                {"name": "b", "rules": [{"alert": "Two", "annotations": {}}]},
            ]},
        }
        self.assertEqual([n for n, _ in iter_alerts([doc])], ["One", "Two"])

    def test_ignores_recording_rules(self):
        # A rule with `record` and no `alert` has nobody to page and needs no runbook.
        doc = {"kind": "PrometheusRule", "spec": {"groups": [
            {"name": "g", "rules": [{"record": "job:foo", "expr": "1"}]}]}}
        self.assertEqual(list(iter_alerts([doc])), [])

    def test_ignores_other_kinds(self):
        self.assertEqual(list(iter_alerts([{"kind": "ConfigMap"}])), [])


class TestRunbookSlug(unittest.TestCase):
    def test_extracts_the_filename(self):
        self.assertEqual(runbook_slug(BASE_URL + "cache-degraded.md"), "cache-degraded.md")

    def test_strips_an_anchor(self):
        self.assertEqual(
            runbook_slug(BASE_URL + "cache-degraded.md#triage"), "cache-degraded.md")

    def test_rejects_a_url_pointing_somewhere_else(self):
        self.assertIsNone(runbook_slug("https://example.com/some/wiki/page"))

    def test_rejects_an_empty_url(self):
        self.assertIsNone(runbook_slug(""))


class TestCheck(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        os.makedirs(os.path.join(self.root, "docs/observability/runbooks"))
        self._write("real.md")

    def _write(self, name):
        path = os.path.join(self.root, "docs/observability/runbooks", name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write("# runbook\n")

    def test_passes_when_the_runbook_exists(self):
        docs = [rule_doc(("Alert", {"runbook_url": BASE_URL + "real.md"}))]
        failures, linked = check(docs, self.root)
        self.assertEqual(failures, [])
        self.assertEqual(linked, {"real.md"})

    def test_reports_a_missing_runbook(self):
        # The failure this whole script exists for: the annotation is present, the link is
        # well-formed, and following it at 3am gives you a 404.
        docs = [rule_doc(("Alert", {"runbook_url": BASE_URL + "ghost.md"}))]
        failures, _ = check(docs, self.root)
        self.assertTrue(any("ghost.md" in f and "does not exist" in f for f in failures), failures)

    def test_reports_an_alert_with_no_runbook_url(self):
        docs = [rule_doc(("Alert", {"summary": "something happened"}))]
        failures, _ = check(docs, self.root)
        self.assertTrue(any("no runbook_url" in f for f in failures), failures)

    def test_reports_a_url_pointing_outside_the_runbook_directory(self):
        docs = [rule_doc(("Alert", {"runbook_url": "https://example.com/wiki"}))]
        failures, _ = check(docs, self.root)
        self.assertTrue(any("does not point under" in f for f in failures), failures)

    def test_reports_every_defect_not_just_the_first(self):
        docs = [rule_doc(
            ("A", {"runbook_url": BASE_URL + "ghost.md"}),
            ("B", {}),
        )]
        failures, _ = check(docs, self.root)
        self.assertEqual(len(failures), 2)

    def test_orphans_are_found_but_are_not_failures(self):
        self._write("left-behind.md")
        docs = [rule_doc(("Alert", {"runbook_url": BASE_URL + "real.md"}))]
        failures, linked = check(docs, self.root)
        self.assertEqual(failures, [])
        self.assertEqual(orphans(self.root, linked), ["left-behind.md"])


class TestMainGuards(unittest.TestCase):
    """Reading nothing must be an error, not a pass."""

    def test_no_alerts_is_an_error(self):
        with tempfile.TemporaryDirectory() as rules:
            with open(os.path.join(rules, "empty.yaml"), "w", encoding="utf-8") as fh:
                fh.write("kind: ConfigMap\n")
            self.assertEqual(main([f"--rules-dir={rules}"]), 1)

    def test_a_missing_rules_directory_is_an_error(self):
        self.assertEqual(main(["--rules-dir=/nonexistent/rules"]), 1)


if __name__ == "__main__":
    unittest.main()
