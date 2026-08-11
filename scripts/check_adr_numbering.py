#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if two ADRs share a number, or if an ADR disagrees with itself.

Three separate collisions happened while one branch was open: a 0006, then a
0010/0011/0012 three-way. Each was found by a person reading a merge conflict, and each
could just as easily have been resolved by taking one side — leaving two ADR 0010s in the
tree, two decisions with one identity, and every `Superseded by 0012` reference ambiguous
forever.

**Nothing detected any of them.** The index table says "Keep this table in sync whenever you
add or change an ADR's status", which is prose, and prose does not fail a build.

The collisions are structural rather than careless. Two branches each take "one above the
highest in the index", as `docs/adr/README.md` instructs, and both are right when they do it.
They only conflict on the way in — which is exactly where this gate sits: on the branch that
merges second, both files exist, and the duplicate becomes visible at the one moment it can
still be fixed cheaply.

## What is rejected

  * **Two files claiming the same number.** The collision itself.
  * **A heading that disagrees with the filename.** Renumbering is a rename plus edits to the
    heading, the front-matter id and every inbound link; doing three of the four leaves an
    `0013-*.md` that calls itself ADR 0010, which is worse than either number alone.
  * **A front-matter `id` that disagrees with the filename**, where one is present. Four of
    the fifteen ADRs carry front matter, so this is checked when found rather than required.
  * **An index that has drifted**: an ADR with no row, a row pointing at a file that does not
    exist, or a row whose link target does not match its number.
  * **A dangling relative link between ADRs.** Renumbering breaks inbound links silently —
    `[ADR 0011](0011-cache-first-unread-count.md)` keeps rendering as text after the file
    becomes `0014-`, and only a reader clicking it finds out.

## What is deliberately not rejected

**Gaps in the sequence.** A withdrawn ADR leaves one legitimately, and the repo's own policy
is that ADRs are an append-only log rather than a renumbered-to-be-tidy one. Failing on gaps
would push someone toward renumbering history, which is the thing the policy forbids.

Usage:
    check_adr_numbering.py [docs/adr]
"""

import os
import re
import sys

FILENAME = re.compile(r"^(\d{4})-(.+)\.md$")
HEADING = re.compile(r"^#\s+ADR\s+(\d+)\s*[::]", re.M)
FRONT_MATTER_ID = re.compile(r"^id:\s*(\d+)\s*$", re.M)
# Markdown links to a sibling ADR file, e.g. [ADR 0014](0014-cache-first-unread-count.md).
ADR_LINK = re.compile(r"\]\((\d{4}-[^)#]+\.md)(?:#[^)]*)?\)")
# Index rows: | [0013](0013-....md) | Title | Status | Date |
INDEX_ROW = re.compile(r"^\|\s*\[(\d{4})\]\((\d{4}-[^)]+\.md)\)", re.M)

SKIP = {"README.md", "template.md"}


def adr_files(directory):
    """(number, filename) for every ADR, ignoring the README and the template."""
    out = []
    for name in sorted(os.listdir(directory)):
        if name in SKIP or not name.endswith(".md"):
            continue
        m = FILENAME.match(name)
        if m:
            out.append((m.group(1), name))
    return out


def evaluate(directory):
    """Return (failures, count of ADRs checked).

    Split out from main() so the decision is testable without a filesystem full of fixtures,
    as the other gates in this directory do it.
    """
    failures = []
    files = adr_files(directory)
    names = {name for _, name in files}

    # 1. The collision itself.
    seen = {}
    for number, name in files:
        seen.setdefault(number, []).append(name)
    for number, claimants in sorted(seen.items()):
        if len(claimants) > 1:
            failures.append(
                f"ADR {number} is claimed by {len(claimants)} files ({', '.join(claimants)}). "
                "Two decisions cannot share one identity: every reference to that number, and "
                "every 'Superseded by' pointing at it, becomes ambiguous. Renumber the one that "
                "landed second."
            )

    # 2 and 3. A file that disagrees with itself.
    for number, name in files:
        with open(os.path.join(directory, name), encoding="utf-8") as handle:
            body = handle.read()

        heading = HEADING.search(body)
        if not heading:
            failures.append(f"{name} has no '# ADR NNNN:' heading, so its number is unverifiable")
        elif heading.group(1) != number:
            failures.append(
                f"{name} is titled 'ADR {heading.group(1)}' but its filename says {number}. "
                "A renumber renames the file, the heading, the front-matter id and every inbound "
                "link; this one stopped partway."
            )

        front = FRONT_MATTER_ID.search(body)
        if front and front.group(1) != number:
            failures.append(
                f"{name} has front-matter id {front.group(1)} but its filename says {number}"
            )

        # 5. Inbound links that a renumber silently broke.
        for target in ADR_LINK.findall(body):
            if target not in names:
                failures.append(
                    f"{name} links to {target}, which does not exist — a renumber that missed "
                    "an inbound link. The text still renders; only a reader clicking it finds out."
                )

    # 4. The index, which prose alone has never kept in sync.
    readme = os.path.join(directory, "README.md")
    if os.path.exists(readme):
        with open(readme, encoding="utf-8") as handle:
            body = handle.read()
        rows = INDEX_ROW.findall(body)
        listed = {number for number, _ in rows}

        for number, target in rows:
            if target not in names:
                failures.append(f"the index links ADR {number} to {target}, which does not exist")
            elif not target.startswith(number):
                failures.append(
                    f"the index row for ADR {number} links to {target}, a different ADR"
                )

        for number, name in files:
            if number not in listed:
                failures.append(
                    f"{name} has no row in the index. 'Keep this table in sync' is the "
                    "instruction that has never been enforced."
                )

    return failures, len(files)


def report(failures, checked):
    """Print the verdict and return the exit code. Separated so the sentinel is testable."""
    # Checking nothing is the failure mode every gate here guards against.
    if checked == 0:
        print(
            "ERROR: no ADRs found; is this the right directory? Point the gate at docs/adr "
            "rather than letting it pass by checking nothing.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} problem(s) across {checked} ADRs:\n", file=sys.stderr)
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            "\nADR numbers are identities: they appear in 'Superseded by', in cross-references,\n"
            "and in commit messages that outlive the files. Two branches each taking 'one above\n"
            "the highest' are both following docs/adr/README.md correctly — the duplicate only\n"
            "exists once they meet, which is here.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: {checked} ADRs have distinct numbers, agree with their headings, and are indexed")
    return 0


def main(argv):
    directory = argv[0] if argv else "docs/adr"
    if not os.path.isdir(directory):
        print(f"ERROR: {directory} is not a directory", file=sys.stderr)
        return 1
    return report(*evaluate(directory))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
