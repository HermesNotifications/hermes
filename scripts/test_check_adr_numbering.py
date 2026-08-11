#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Tests for the ADR numbering check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import os
import tempfile
import unittest

import check_adr_numbering as check


def adr(number, slug, heading=None, front_id=None, body=""):
    """An ADR file's (name, contents). `heading` defaults to agreeing with `number`."""
    head = heading if heading is not None else number
    text = ""
    if front_id is not None:
        text += f"---\nid: {front_id}\ntitle: Something\n---\n\n"
    text += f"# ADR {head}: A decision\n\n{body}\n"
    return f"{number}-{slug}.md", text


def index(*rows):
    """A README with an index table listing `rows` of (number, filename)."""
    lines = [
        "# Architecture Decision Records",
        "",
        "## Index",
        "",
        "| # | Title | Status | Date |",
        "|---|---|---|---|",
    ]
    for number, target in rows:
        lines.append(f"| [{number}]({target}) | A decision | Accepted | 2026-08-11 |")
    return "README.md", "\n".join(lines) + "\n"


class Fixture:
    """A throwaway docs/adr directory."""

    def __init__(self, files):
        self.dir = tempfile.mkdtemp()
        for name, text in files:
            with open(os.path.join(self.dir, name), "w", encoding="utf-8") as handle:
                handle.write(text)


def evaluate(*files):
    return check.evaluate(Fixture(files).dir)


class TestTheCollision(unittest.TestCase):
    """The defect this exists for: two branches each taking 'one above the highest'."""

    def test_two_files_claiming_one_number_fails(self):
        failures, checked = evaluate(
            adr("0010", "bounded-work-streams"),
            adr("0010", "embeddable-inbox-widget-contract"),
            index(("0010", "0010-bounded-work-streams.md")),
        )
        self.assertEqual(checked, 2)
        self.assertTrue(any("claimed by 2 files" in f for f in failures))

    def test_a_three_way_collision_reports_each_number(self):
        failures, _ = evaluate(
            adr("0010", "a"), adr("0010", "b"),
            adr("0011", "c"), adr("0011", "d"),
            adr("0012", "e"), adr("0012", "f"),
            index(),
        )
        collisions = [f for f in failures if "claimed by 2 files" in f]
        self.assertEqual(len(collisions), 3)

    def test_distinct_numbers_pass(self):
        failures, checked = evaluate(
            adr("0010", "a"), adr("0011", "b"),
            index(("0010", "0010-a.md"), ("0011", "0011-b.md")),
        )
        self.assertEqual(failures, [])
        self.assertEqual(checked, 2)


class TestHalfFinishedRenumber(unittest.TestCase):
    """Renaming the file is one of four edits. Stopping partway is the likely mistake."""

    def test_heading_disagreeing_with_filename_fails(self):
        failures, _ = evaluate(
            adr("0013", "embeddable-inbox", heading="0010"),
            index(("0013", "0013-embeddable-inbox.md")),
        )
        self.assertTrue(any("titled 'ADR 0010'" in f for f in failures))

    def test_front_matter_id_disagreeing_with_filename_fails(self):
        failures, _ = evaluate(
            adr("0013", "embeddable-inbox", front_id="0010"),
            index(("0013", "0013-embeddable-inbox.md")),
        )
        self.assertTrue(any("front-matter id 0010" in f for f in failures))

    def test_front_matter_is_optional(self):
        # Only four of the fifteen real ADRs carry it, so absence must not fail.
        failures, _ = evaluate(
            adr("0013", "embeddable-inbox"),
            index(("0013", "0013-embeddable-inbox.md")),
        )
        self.assertEqual(failures, [])

    def test_a_missing_heading_fails(self):
        name = "0013-embeddable-inbox.md"
        failures, _ = evaluate((name, "No heading here at all.\n"), index(("0013", name)))
        self.assertTrue(any("no '# ADR NNNN:' heading" in f for f in failures))

    def test_a_link_broken_by_a_renumber_fails(self):
        # The silent one: the text keeps rendering, so only a reader clicking it finds out.
        failures, _ = evaluate(
            adr("0014", "cache-first", body="See [ADR 0011](0011-cache-first-unread-count.md)."),
            index(("0014", "0014-cache-first.md")),
        )
        self.assertTrue(any("does not exist" in f for f in failures))

    def test_a_link_to_an_existing_adr_passes(self):
        failures, _ = evaluate(
            adr("0014", "cache-first", body="See [ADR 0013](0013-inbox.md)."),
            adr("0013", "inbox"),
            index(("0013", "0013-inbox.md"), ("0014", "0014-cache-first.md")),
        )
        self.assertEqual(failures, [])

    def test_a_link_with_an_anchor_passes(self):
        failures, _ = evaluate(
            adr("0014", "cache-first", body="See [ADR 0013](0013-inbox.md#decision)."),
            adr("0013", "inbox"),
            index(("0013", "0013-inbox.md"), ("0014", "0014-cache-first.md")),
        )
        self.assertEqual(failures, [])


class TestTheIndex(unittest.TestCase):
    """'Keep this table in sync' is prose, and prose does not fail a build."""

    def test_an_adr_with_no_row_fails(self):
        failures, _ = evaluate(adr("0013", "inbox"), index())
        self.assertTrue(any("no row in the index" in f for f in failures))

    def test_a_row_pointing_at_a_missing_file_fails(self):
        failures, _ = evaluate(
            adr("0013", "inbox"),
            index(("0013", "0013-inbox.md"), ("0014", "0014-gone.md")),
        )
        self.assertTrue(any("does not exist" in f for f in failures))

    def test_a_row_linking_to_a_different_adr_fails(self):
        # The copy-paste slip: right number in the cell, previous ADR's file in the link.
        failures, _ = evaluate(
            adr("0013", "inbox"), adr("0014", "cache"),
            index(("0013", "0013-inbox.md"), ("0014", "0013-inbox.md")),
        )
        self.assertTrue(any("a different ADR" in f for f in failures))


class TestDeliberateNonFailures(unittest.TestCase):
    def test_a_gap_in_the_sequence_passes(self):
        # A withdrawn ADR leaves one legitimately, and the repo's policy is append-only.
        # Failing here would push someone toward renumbering history, which that policy forbids.
        failures, _ = evaluate(
            adr("0010", "a"), adr("0012", "c"),
            index(("0010", "0010-a.md"), ("0012", "0012-c.md")),
        )
        self.assertEqual(failures, [])

    def test_the_template_and_readme_are_not_treated_as_adrs(self):
        failures, checked = evaluate(
            adr("0010", "a"),
            ("template.md", "# ADR NNNN: Title\n"),
            index(("0010", "0010-a.md")),
        )
        self.assertEqual(checked, 1)
        self.assertEqual(failures, [])


class TestSentinels(unittest.TestCase):
    def test_an_empty_directory_is_an_error_not_a_pass(self):
        self.assertEqual(check.report([], 0), 1)

    def test_a_clean_run_reports_ok(self):
        self.assertEqual(check.report([], 15), 0)


if __name__ == "__main__":
    unittest.main()
