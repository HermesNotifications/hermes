#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Scaffold a new ADR, allocating a number that is free everywhere it needs to be.

Picking "the next one" by looking at your own `docs/adr/` is what causes collisions. Your
directory is a snapshot of the main you branched from, and it cannot see the ADR that landed on
main yesterday, nor the one sitting in an open PR that will land before yours.

So this looks in three places:

  1. **The working tree** — what you already have.
  2. **`origin/main`** — what has landed since you branched. Fetched first, because a stale
     remote-tracking ref is the same blind spot in a different coat.
  3. **Open pull requests** — what is about to land. This is the one that catches the case the
     other two cannot: two branches, both correct against main, both adding the same number.
     Needs `gh` and network; skipped with a warning when either is missing, because a scaffolding
     command that refuses to run offline is a command people work around.

None of this is a lock. Two people running it in the same minute still race, and an ADR written
without it still collides. `scripts/check_adr_numbering.py` is the backstop that catches both.

Usage:
    adr_new.py "Bound the JetStream work streams"
    adr_new.py --number-only
"""

import argparse
import json
import pathlib
import re
import subprocess
import sys
from datetime import date, timezone, datetime

FILENAME_RE = re.compile(r"^(\d{4})-[a-z0-9][a-z0-9-]*\.md$")
NON_ADR = {"README.md", "template.md"}


def slugify(title):
    """A filename slug: lowercase, alphanumerics and hyphens, no runs, no edges."""
    slug = re.sub(r"[^a-z0-9]+", "-", title.lower()).strip("-")
    if not slug:
        raise SystemExit("error: the title produced an empty slug; use some letters or digits")
    return slug


def local_numbers(adr_dir):
    out = set()
    for path in pathlib.Path(adr_dir).glob("*.md"):
        if path.name in NON_ADR:
            continue
        m = FILENAME_RE.match(path.name)
        if m:
            out.add(int(m.group(1)))
    return out


def _run(cmd, timeout=30):
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, check=True).stdout


def main_numbers(adr_dir, base_ref, fetch=True):
    """Numbers on the base ref. Returns (numbers, note) where note explains any gap."""
    if fetch and "/" in base_ref:
        remote, branch = base_ref.split("/", 1)
        try:
            _run(["git", "fetch", "--quiet", remote, branch], timeout=60)
        except Exception:
            pass  # An unfetchable remote is reported by the ls-tree below, not here.
    try:
        out = _run(["git", "ls-tree", "--name-only", base_ref, f"{adr_dir}/"])
    except Exception:
        return set(), f"could not read {base_ref}; an ADR that landed there since you branched is invisible"
    numbers = set()
    for line in out.splitlines():
        m = FILENAME_RE.match(pathlib.PurePath(line.strip()).name)
        if m:
            numbers.add(int(m.group(1)))
    return numbers, None


def open_pr_numbers(adr_dir):
    """Numbers claimed by ADRs in open PRs. Returns (numbers, note).

    Counts every ADR path a PR *touches*, not only the ones it adds, because gh does not
    distinguish them here. That over-counts: a PR amending ADR 0005 marks 0005 taken. It is
    already taken -- it is on main -- so the only effect is to keep a conservative allocator
    conservative, and the alternative (per-PR diff inspection) is a lot of API calls to avoid a
    consequence that does not exist.
    """
    try:
        out = _run(["gh", "pr", "list", "--state", "open", "--limit", "100", "--json", "number,files"], timeout=60)
    except FileNotFoundError:
        return set(), "gh is not installed, so ADRs in open PRs are invisible"
    except Exception:
        return set(), "could not list open PRs (not authenticated, or offline), so ADRs in them are invisible"

    numbers = set()
    try:
        for pr in json.loads(out):
            for f in pr.get("files") or []:
                path = f.get("path", "")
                if not path.startswith(f"{adr_dir}/"):
                    continue
                m = FILENAME_RE.match(pathlib.PurePath(path).name)
                if m:
                    numbers.add(int(m.group(1)))
    except (json.JSONDecodeError, TypeError):
        return set(), "could not parse the open-PR listing, so ADRs in them are invisible"
    return numbers, None


def allocate(taken):
    candidate = 1
    while candidate in taken:
        candidate += 1
    return candidate


def render(template_path, number, title, author):
    text = pathlib.Path(template_path).read_text(encoding="utf-8")
    text = text.replace(
        "# ADR NNNN: <short imperative title — the decision, not the problem>",
        f"# ADR {number:04d}: {title}",
    )
    # Status is left at the template's "Proposed" on purpose: an ADR is Accepted when a reviewer
    # says so, not when it is scaffolded.
    text = text.replace("**Date:** YYYY-MM-DD", f"**Date:** {date.today().isoformat()}")
    text = text.replace("**Author:** <name>", f"**Author:** {author}")
    return text


def git_author():
    try:
        return _run(["git", "config", "user.name"]).strip() or "<name>"
    except Exception:
        return "<name>"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("title", nargs="?", help="Short imperative title — the decision, not the problem")
    parser.add_argument("--adr-dir", default="docs/adr")
    parser.add_argument("--base", default="origin/main")
    parser.add_argument("--no-fetch", action="store_true", help="Skip the git fetch of the base ref")
    parser.add_argument("--no-prs", action="store_true", help="Skip the open-PR scan (offline, or in a hurry)")
    parser.add_argument("--number-only", action="store_true", help="Print the next free number and exit")
    args = parser.parse_args(argv)

    adr_dir = pathlib.Path(args.adr_dir)
    if not adr_dir.is_dir():
        raise SystemExit(f"error: {adr_dir} is not a directory")

    notes = []
    taken = local_numbers(adr_dir)

    base, note = main_numbers(args.adr_dir, args.base, fetch=not args.no_fetch)
    taken |= base
    if note:
        notes.append(note)

    if not args.no_prs:
        prs, note = open_pr_numbers(args.adr_dir)
        taken |= prs
        if note:
            notes.append(note)

    number = allocate(taken)

    for n in notes:
        print(f"warning: {n}", file=sys.stderr)
    if notes:
        print("warning: the number below may still collide; check_adr_numbering.py will catch it if so.",
              file=sys.stderr)

    if args.number_only:
        print(f"{number:04d}")
        return 0

    if not args.title:
        parser.error("a title is required unless --number-only is given")

    path = adr_dir / f"{number:04d}-{slugify(args.title)}.md"
    if path.exists():
        raise SystemExit(f"error: {path} already exists")

    template = adr_dir / "template.md"
    if not template.exists():
        raise SystemExit(f"error: {template} is missing; nothing to scaffold from")

    path.write_text(render(template, number, args.title, git_author()), encoding="utf-8")

    print(f"created {path}")
    print(
        f"\nStill to do by hand, because neither is mechanical:\n"
        f"  1. Add the row to {adr_dir}/README.md's index table.\n"
        f"  2. Write the thing.\n"
        f"\n`make verify` will tell you if you skip (1)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
