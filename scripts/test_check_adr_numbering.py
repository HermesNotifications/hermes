#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the ADR numbering check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import unittest

import check_adr_numbering as check


def files(*pairs):
    """[(number, filename)] from ("0001", "slug") pairs."""
    return [(n, f"{n}-{slug}.md") for n, slug in pairs]


def headings(file_list, override=None):
    """Every file's heading matching its filename, unless overridden."""
    out = {name: number for number, name in file_list}
    if override:
        out.update(override)
    return out


def evaluate(file_list, malformed=(), heading_override=None, index=None, base=None):
    return check.evaluate(
        file_list,
        list(malformed),
        headings(file_list, heading_override),
        file_list if index is None else index,
        base,
    )


class CleanTree(unittest.TestCase):
    def test_a_consistent_tree_passes(self):
        f = files(("0001", "first"), ("0002", "second"))
        failures, stats = evaluate(f, base={"0001": "0001-first.md", "0002": "0002-second.md"})
        self.assertEqual(failures, [])
        self.assertEqual(stats["adrs"], 2)

    def test_gaps_are_allowed(self):
        # A withdrawn ADR leaves a hole. Closing it would break every reference to the ADRs
        # after it, which is a worse outcome than a non-contiguous sequence.
        failures, _ = evaluate(files(("0001", "first"), ("0004", "fourth")))
        self.assertEqual(failures, [])


class InTreeProblems(unittest.TestCase):
    def test_two_files_claiming_one_number(self):
        f = [("0007", "0007-one.md"), ("0007", "0007-two.md")]
        failures, _ = check.evaluate(f, [], headings(f), f, None)
        self.assertTrue(any("claimed by 2 files" in x for x in failures))

    def test_heading_disagreeing_with_filename(self):
        f = files(("0007", "seven"))
        failures, _ = evaluate(f, heading_override={"0007-seven.md": "0008"})
        self.assertTrue(any("`# ADR 0008:`" in x for x in failures))

    def test_missing_heading(self):
        f = files(("0007", "seven"))
        failures, _ = evaluate(f, heading_override={"0007-seven.md": None})
        self.assertTrue(any("no `# ADR NNNN:` heading" in x for x in failures))

    def test_malformed_filename(self):
        failures, _ = evaluate(files(("0001", "first")), malformed=["adr-7-notes.md"])
        self.assertTrue(any("adr-7-notes.md" in x and "NNNN-slug.md" in x for x in failures))


class IndexDrift(unittest.TestCase):
    def test_an_adr_missing_from_the_index(self):
        f = files(("0001", "first"), ("0002", "second"))
        failures, _ = evaluate(f, index=files(("0001", "first")))
        self.assertTrue(any("0002-second.md is not in README.md's index" in x for x in failures))

    def test_an_index_row_pointing_at_no_file(self):
        f = files(("0001", "first"))
        failures, _ = evaluate(f, index=files(("0001", "first"), ("0009", "ghost")))
        self.assertTrue(any("indexes ADR 0009" in x for x in failures))

    def test_an_index_row_naming_the_wrong_file(self):
        # The shape a rename leaves when the table is not updated with it.
        f = files(("0001", "renamed"))
        failures, _ = evaluate(f, index=[("0001", "0001-old-name.md")])
        self.assertTrue(any("indexes ADR 0001 as 0001-old-name.md" in x for x in failures))


class CrossBranchCollision(unittest.TestCase):
    """The case this gate exists for, and the only one that is invisible locally."""

    def test_same_number_different_decision_on_base(self):
        f = files(("0011", "cache-first-unread-count"))
        failures, _ = evaluate(f, base={"0011": "0011-api-keys-are-scoped-to-an-organization.md"})
        self.assertEqual(len(failures), 1)
        self.assertIn("means two different decisions", failures[0])

    def test_same_number_same_file_is_not_a_collision(self):
        # An ADR that already exists on the base is one this branch did not add.
        f = files(("0011", "same"))
        failures, _ = evaluate(f, base={"0011": "0011-same.md"})
        self.assertEqual(failures, [])

    def test_a_number_free_on_base_is_fine(self):
        f = files(("0013", "new-decision"))
        failures, _ = evaluate(f, base={"0011": "0011-something-else.md"})
        self.assertEqual(failures, [])

    def test_the_check_is_skipped_when_the_base_cannot_be_read(self):
        # None means "could not look", which must not be reported as a pass.
        f = files(("0011", "cache-first-unread-count"))
        failures, stats = evaluate(f, base=None)
        self.assertEqual(failures, [])
        self.assertIsNone(stats["base_adrs"])

    def test_the_real_pr_73_collision(self):
        # Reproduces exactly what shipped: three numbers, each meaning something different on
        # main. Found by reading, which is why this gate exists.
        f = files(
            ("0010", "embeddable-inbox-widget-contract"),
            ("0011", "cache-first-unread-count"),
            ("0012", "lifecycle-and-jetstream-durability"),
        )
        base = {
            "0010": "0010-bounded-work-streams-reject-rather-than-drop.md",
            "0011": "0011-api-keys-are-scoped-to-an-organization.md",
            "0012": "0012-api-keys-are-not-scoped-to-organizations.md",
        }
        failures, _ = evaluate(f, base=base)
        self.assertEqual(len(failures), 3)


class NextFreeNumber(unittest.TestCase):
    def test_accounts_for_both_sides(self):
        f = files(("0001", "a"), ("0002", "b"))
        self.assertEqual(check.next_free_number(f, {"0003": "0003-c.md"}), "0004")

    def test_fills_a_gap(self):
        # Deliberate: the hint is for someone renumbering, and the lowest free number is the
        # one least likely to collide with a third branch.
        f = files(("0001", "a"), ("0003", "c"))
        self.assertEqual(check.next_free_number(f, {}), "0002")

    def test_handles_an_absent_base(self):
        self.assertEqual(check.next_free_number(files(("0001", "a")), None), "0002")


class RequireBase(unittest.TestCase):
    """A green CI step must prove the comparison happened, not merely that nothing objected."""

    def test_an_unreadable_base_is_a_skip_by_default(self):
        stats = {"adrs": 3, "indexed": 3, "base_adrs": None}
        self.assertEqual(check.report([], stats, "origin/main", [], None), 0)

    def test_an_unreadable_base_fails_under_require_base(self):
        stats = {"adrs": 3, "indexed": 3, "base_adrs": None}
        self.assertEqual(check.report([], stats, "origin/main", [], None, require_base=True), 1)

    def test_require_base_is_satisfied_by_a_readable_base(self):
        stats = {"adrs": 3, "indexed": 3, "base_adrs": 3}
        self.assertEqual(check.report([], stats, "origin/main", [], {}, require_base=True), 0)


class Sentinels(unittest.TestCase):
    def test_an_empty_directory_is_an_error_not_a_pass(self):
        # A gate that silently verifies nothing is the failure mode these all exist to prevent.
        code = check.report([], {"adrs": 0, "indexed": 0, "base_adrs": 0}, "origin/main", [], {})
        self.assertEqual(code, 1)

    def test_failures_exit_non_zero(self):
        code = check.report(["something"], {"adrs": 3, "indexed": 3, "base_adrs": 3}, "origin/main", [], {})
        self.assertEqual(code, 1)

    def test_a_clean_tree_exits_zero(self):
        code = check.report([], {"adrs": 3, "indexed": 3, "base_adrs": 3}, "origin/main", [], {})
        self.assertEqual(code, 0)


if __name__ == "__main__":
    unittest.main()
