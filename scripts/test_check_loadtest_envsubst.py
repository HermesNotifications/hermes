#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
"""Tests for check_loadtest_envsubst."""

from __future__ import annotations

import unittest
from pathlib import Path

from check_loadtest_envsubst import (
    MANIFESTS,
    SCRIPT,
    exported_names,
    find_missing,
)

REPO_ROOT = Path(__file__).resolve().parent.parent


class ExportedNames(unittest.TestCase):
    def test_reads_a_line_continued_export_block(self):
        script = "export A B \\\n  C D \\\n  E\n"
        self.assertEqual(exported_names(script), {"A", "B", "C", "D", "E"})

    def test_reads_several_separate_export_statements(self):
        self.assertEqual(exported_names("export A\nexport B C\n"), {"A", "B", "C"})

    def test_ignores_assignments_that_are_not_exports(self):
        self.assertEqual(exported_names(': "${A:=1}"\nB=2\n'), set())


class FindMissing(unittest.TestCase):
    def test_flags_a_referenced_but_unexported_variable(self):
        missing = find_missing("export A\n", {"m.yaml": "x: ${A}\ny: ${B}\n"})
        self.assertEqual(missing, {"m.yaml": ["B"]})

    def test_passes_when_every_reference_is_exported(self):
        self.assertEqual(find_missing("export A B\n", {"m.yaml": "${A}${B}"}), {})

    def test_reports_per_file(self):
        missing = find_missing("export A\n", {"one.yaml": "${B}", "two.yaml": "${C}"})
        self.assertEqual(missing, {"one.yaml": ["B"], "two.yaml": ["C"]})

    def test_an_unused_export_is_not_an_error(self):
        self.assertEqual(find_missing("export A B\n", {"m.yaml": "${A}"}), {})


class AgainstTheRealFiles(unittest.TestCase):
    """The regression itself: nine variables testrun.yaml used and run-k8s.sh never set."""

    def test_repo_manifests_have_no_unexported_variables(self):
        script = (REPO_ROOT / SCRIPT).read_text()
        manifests = {rel: (REPO_ROOT / rel).read_text() for rel in MANIFESTS}
        self.assertEqual(find_missing(script, manifests), {})

    def test_the_nine_that_regressed_are_exported(self):
        exported = exported_names((REPO_ROOT / SCRIPT).read_text())
        for name in (
            "CONNECTIONS",
            "WS_SOCKETS_PER_VU",
            "WS_RAMP_SECONDS",
            "WS_HOLD_SECONDS",
            "CHANNEL_WEIGHTS",
            "RUNNER_CPU_REQ",
            "RUNNER_MEM_REQ",
            "RUNNER_CPU_LIM",
            "RUNNER_MEM_LIM",
        ):
            self.assertIn(name, exported)


if __name__ == "__main__":
    unittest.main()
