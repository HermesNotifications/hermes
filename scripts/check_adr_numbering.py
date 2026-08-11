#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if two ADRs claim the same number, or if the index and the files disagree.

An ADR number is a global identifier allocated at *authoring* time by writers who cannot see
each other. Two long-lived branches each add "the next one" against the main they branched
from, and both are right until they meet. Nothing notices: each branch renders, reviews and
merges cleanly, and the collision only exists relative to a main that neither has met yet.

That happened. PR #73 carried an 0010 while main independently landed a different 0010, and a
branch stacked on #73 added an 0011 and 0012 against main's own 0011 and 0012. Four ADRs, three
collisions, discovered by reading rather than by any tool.

Four things are rejected, and the last is the one that matters.

  * **Two files claiming one number.** The in-tree case. Rare, and usually a copy-paste.

  * **A heading that disagrees with its filename.** `0007-foo.md` opening `# ADR 0008:` is what
    a half-finished renumber leaves behind, and it is invisible in rendered Markdown because
    nobody reads the filename and the heading in the same glance.

  * **Index drift.** An ADR absent from README.md's table is one nobody browsing the directory
    will find; a table row pointing at no file is a dead link. Both directions are checked,
    because the convention in README.md ("Keep this table in sync") was prose.

  * **A number that means something different on `main`.** The real one. Compared against
    `origin/main` rather than the merge base on purpose: what matters is not where the branch
    diverged but what the number will collide with when it lands. Skipped when the ref is
    unavailable (a shallow clone, no remote), because a gate that cannot see main should say so
    rather than pass quietly.

Numbers are NOT required to be contiguous. A gap is what a withdrawn ADR leaves, and renumbering
the survivors to close it would break every reference to them -- the cure being worse than the
cosmetic complaint.

Usage:
    check_adr_numbering.py [--adr-dir docs/adr] [--base origin/main]
"""

import argparse
import pathlib
import re
import subprocess
import sys

# 0007-some-slug.md — four digits, a hyphen, a slug.
FILENAME_RE = re.compile(r"^(\d{4})-([a-z0-9][a-z0-9-]*)\.md$")
# The first-line heading: `# ADR 0007: Title`
HEADING_RE = re.compile(r"^#\s+ADR\s+(\d{4})\s*:", re.MULTILINE)
# An index row: `| [0007](0007-some-slug.md) | Title | Status | Date |`
INDEX_ROW_RE = re.compile(r"^\|\s*\[(\d{4})\]\((\d{4}-[a-z0-9-]+\.md)\)\s*\|", re.MULTILINE)

# Not ADRs; they live in the same directory.
NON_ADR = {"README.md", "template.md"}


def adr_files(adr_dir):
    """(number, filename) for every ADR in the directory, plus any malformed names."""
    found, malformed = [], []
    for path in sorted(pathlib.Path(adr_dir).glob("*.md")):
        if path.name in NON_ADR:
            continue
        m = FILENAME_RE.match(path.name)
        if not m:
            malformed.append(path.name)
            continue
        found.append((m.group(1), path.name))
    return found, malformed


def heading_number(adr_dir, filename):
    """The number in the file's `# ADR NNNN:` heading, or None if it has no such heading."""
    text = (pathlib.Path(adr_dir) / filename).read_text(encoding="utf-8")
    m = HEADING_RE.search(text)
    return m.group(1) if m else None


def index_entries(adr_dir):
    """(number, filename) for every row of README.md's index table."""
    readme = pathlib.Path(adr_dir) / "README.md"
    if not readme.exists():
        return []
    return INDEX_ROW_RE.findall(readme.read_text(encoding="utf-8"))


def base_adrs(base_ref, adr_dir):
    """{number: filename} on `base_ref`, or None when the ref cannot be read.

    None and {} mean different things: an empty mapping is a base with no ADRs, while None is
    "could not look", which the report turns into a visible skip rather than a pass.
    """
    try:
        out = subprocess.run(
            ["git", "ls-tree", "--name-only", base_ref, f"{adr_dir}/"],
            capture_output=True, text=True, timeout=30, check=True,
        ).stdout
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, FileNotFoundError):
        return None

    mapping = {}
    for line in out.splitlines():
        name = pathlib.PurePath(line.strip()).name
        if name in NON_ADR:
            continue
        m = FILENAME_RE.match(name)
        if m:
            mapping[m.group(1)] = name
    return mapping


def evaluate(files, malformed, headings, index, base):
    """Return (failures, stats). `failures` is a list of human-readable strings.

    `headings` maps filename -> heading number (or None). `base` maps number -> filename on the
    base ref, or is None when it could not be read.
    """
    failures = []

    for name in malformed:
        failures.append(
            f"{name} is not named NNNN-slug.md, so its number cannot be read and every other "
            f"check silently skips it"
        )

    by_number = {}
    for number, name in files:
        by_number.setdefault(number, []).append(name)
    for number, names in sorted(by_number.items()):
        if len(names) > 1:
            failures.append(f"ADR {number} is claimed by {len(names)} files: {', '.join(sorted(names))}")

    for number, name in files:
        heading = headings.get(name)
        if heading is None:
            failures.append(f"{name} has no `# ADR NNNN:` heading, so nothing ties its content to its number")
        elif heading != number:
            failures.append(
                f"{name} opens with `# ADR {heading}:` — the filename and the heading disagree, "
                f"which is what a half-finished renumber leaves behind"
            )

    indexed = {number: name for number, name in index}
    for number, name in index:
        if (number, name) not in files:
            failures.append(f"README.md indexes ADR {number} as {name}, which is not a file in this directory")
    for number, name in files:
        if number not in indexed:
            failures.append(f"{name} is not in README.md's index, so nobody browsing the table will find it")
        elif indexed[number] != name:
            failures.append(
                f"README.md indexes ADR {number} as {indexed[number]} but the file is {name}"
            )

    if base is not None:
        for number, name in sorted(files):
            other = base.get(number)
            if other is not None and other != name:
                failures.append(
                    f"ADR {number} is {name} here but {other} on the base branch — the number "
                    f"means two different decisions, and merging would leave one of them "
                    f"unreachable by its own identifier"
                )

    stats = {
        "adrs": len(files),
        "indexed": len(index),
        "base_adrs": None if base is None else len(base),
    }
    return failures, stats


def next_free_number(files, base):
    """The lowest four-digit number free in both this tree and the base. For the fix hint."""
    taken = {int(n) for n, _ in files}
    if base:
        taken |= {int(n) for n in base}
    candidate = 1
    while candidate in taken:
        candidate += 1
    return f"{candidate:04d}"


def report(failures, stats, base_ref, files, base):
    if stats["adrs"] == 0:
        print("ERROR: no ADRs found; is --adr-dir pointing at the right directory?", file=sys.stderr)
        return 1

    if stats["base_adrs"] is None:
        print(
            f"note: could not read {base_ref}, so the cross-branch collision check did not run. "
            f"Fetch it (`git fetch origin main`) to get the check that actually matters.",
            file=sys.stderr,
        )

    if failures:
        print(f"FAIL: {len(failures)} problem(s) with the ADR numbering:\n", file=sys.stderr)
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            f"\nTo renumber an ADR: `git mv` the file to its new number, update the `# ADR NNNN:`\n"
            f"heading inside it, update its row in README.md, and update anything referring to it\n"
            f"(`grep -rn 'ADR 00NN' docs/ internal/ charts/ deploy/`). The next free number in\n"
            f"both this tree and {base_ref} is {next_free_number(files, base)}.\n"
            f"\n`make adr-new` allocates against both, which is how not to arrive here again.",
            file=sys.stderr,
        )
        return 1

    scope = "" if stats["base_adrs"] is None else f", none colliding with {base_ref}"
    print(f"ok: {stats['adrs']} ADRs, each uniquely numbered and indexed{scope}")
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--adr-dir", default="docs/adr")
    parser.add_argument("--base", default="origin/main",
                        help="Ref to check numbers against. Use '' to skip that check.")
    args = parser.parse_args(argv)

    files, malformed = adr_files(args.adr_dir)
    headings = {name: heading_number(args.adr_dir, name) for _, name in files}
    index = index_entries(args.adr_dir)
    base = base_adrs(args.base, args.adr_dir) if args.base else None

    failures, stats = evaluate(files, malformed, headings, index, base)
    return report(failures, stats, args.base or "(skipped)", files, base)


if __name__ == "__main__":
    sys.exit(main())
