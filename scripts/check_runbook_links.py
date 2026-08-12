#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if an alert rule has no runbook, or a runbook_url points at nothing.

`deploy/observability/base/prometheus-rules/*.yaml` opens with:

    Every alert MUST have a matching runbook at docs/observability/runbooks/<slug>.md
    (CI check enforces this pairing).

No such check existed. The claim had been sitting at the top of the file, in capitals,
enforcing nothing — the same class of defect as the NetworkPolicy selectors that matched no
pods and the render gate that only ever ran against the chart.

It matters at exactly the wrong moment. A `runbook_url` is read by whoever is paged, at 3am,
by following the link. A 404 there costs the responder the minutes in which the annotation
was supposed to save them, and nothing before that point would have noticed: Prometheus
neither validates the URL nor cares whether it resolves.

Checks, per alert:

  * it has a runbook_url annotation at all;
  * the URL points under docs/observability/runbooks/;
  * that file exists in this repository.

And in the other direction, reported but not fatal: runbooks nothing links to. Those are
usually a rename that left the old file behind, not a defect worth failing a build over.

Usage:
    check_runbook_links.py [--rules-dir=PATH] [--docs-root=PATH]
"""

import os
import sys

RULES_DIR = "deploy/observability/base/prometheus-rules"
RUNBOOK_DIR = "docs/observability/runbooks"
RUNBOOK_MARKER = "docs/observability/runbooks/"


def iter_alerts(docs):
    """(alert name, annotations) for every rule that is an alert."""
    for doc in docs:
        if not doc or doc.get("kind") != "PrometheusRule":
            continue
        for group in (doc.get("spec") or {}).get("groups") or []:
            for rule in group.get("rules") or []:
                name = rule.get("alert")
                if name:
                    yield name, (rule.get("annotations") or {})


def runbook_slug(url):
    """The runbook filename a runbook_url points at, or None if it points elsewhere."""
    if not url or RUNBOOK_MARKER not in url:
        return None
    return url.split(RUNBOOK_MARKER, 1)[1].split("#", 1)[0].strip("/")


def check(docs, docs_root):
    failures = []
    linked = set()

    for name, annotations in iter_alerts(docs):
        url = annotations.get("runbook_url")
        if not url:
            failures.append(f"alert {name} has no runbook_url annotation")
            continue

        slug = runbook_slug(url)
        if slug is None:
            failures.append(
                f"alert {name} has a runbook_url that does not point under "
                f"{RUNBOOK_MARKER}: {url}"
            )
            continue

        linked.add(slug)
        if not os.path.isfile(os.path.join(docs_root, RUNBOOK_DIR, slug)):
            failures.append(
                f"alert {name} links to {RUNBOOK_DIR}/{slug}, which does not exist"
            )

    return failures, linked


def orphans(docs_root, linked):
    """Runbooks no alert links to. Advisory: a rename leaves these behind."""
    path = os.path.join(docs_root, RUNBOOK_DIR)
    if not os.path.isdir(path):
        return []
    return sorted(
        f for f in os.listdir(path)
        if f.endswith(".md") and f != "README.md" and f not in linked
    )


def main(argv):
    rules_dir, docs_root = RULES_DIR, "."
    for arg in argv:
        if arg.startswith("--rules-dir="):
            rules_dir = arg.split("=", 1)[1]
        elif arg.startswith("--docs-root="):
            docs_root = arg.split("=", 1)[1]

    try:
        import yaml
    except ImportError:
        # Skipping when the tool is missing is how a gate quietly stops running.
        print("ERROR: PyYAML not installed; cannot check runbook links", file=sys.stderr)
        return 1

    if not os.path.isdir(rules_dir):
        print(f"ERROR: no rules directory at {rules_dir!r}", file=sys.stderr)
        return 1

    docs = []
    for entry in sorted(os.listdir(rules_dir)):
        if entry.endswith((".yaml", ".yml")):
            with open(os.path.join(rules_dir, entry), encoding="utf-8") as fh:
                docs.extend(d for d in yaml.safe_load_all(fh) if d)

    alerts = list(iter_alerts(docs))
    if not alerts:
        # A gate that reads nothing and reports success is worse than no gate.
        print(f"ERROR: no alert rules found under {rules_dir!r}", file=sys.stderr)
        return 1

    failures, linked = check(docs, docs_root)

    for name in orphans(docs_root, linked):
        print(f"note: {RUNBOOK_DIR}/{name} is not linked from any alert", file=sys.stderr)

    if failures:
        print(f"FAIL: {len(failures)} runbook problems:\n", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        print(
            "\nEvery alert must ship with its runbook in the same PR (CLAUDE.md).\n"
            "A runbook_url is followed by whoever is paged; a 404 costs them the minutes\n"
            "the annotation exists to save.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: {len(alerts)} alerts, every runbook_url resolves")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
