#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a dashboard or alert queries a `hermes_*` metric no Go file emits.

The defect this catches shipped and survived. `pipeline-overview.json` had four panels;
three queried `hermes_notifications_sent_total`, `hermes_notifications_delivered_total`,
`hermes_deliveries_failed_total` and `hermes_template_cache_hits_total`, and no Go file
has ever created an instrument by any of those names. The dashboard drew nothing, and had
done since it was written.

Nothing noticed because the failure is silent in both directions: Grafana renders a panel
with no series exactly like a panel whose service is idle, and Prometheus evaluates an
alert over a metric that does not exist as "not firing" — which is indistinguishable from
healthy. An observability stack that is broken this way looks *better* than one that
works.

The check runs in one direction only, deliberately. A query naming a metric nobody emits
is always a defect. An instrument nobody queries is usually one too — three existed at the
time this was written — but it is a judgement call, and a build is the wrong place to make
it. Those are reported as notes.

Prometheus names are derived from OTel names by the exporter: dots become underscores,
counters gain `_total`, histograms gain a unit suffix and a `_bucket`/`_count`/`_sum`
series. This reverses that mapping approximately, and errs toward accepting: a false pass
costs nothing, a false failure blocks a build for a naming rule the exporter owns.

Usage:
    check_metric_references.py [--root=PATH]
"""

import os
import re
import sys

GO_DIRS = ("internal", "cmd")
QUERY_DIRS = (
    "deploy/observability/base/grafana/dashboards",
    "deploy/observability/base/prometheus-rules",
)

# Instrument creation: meter.Int64Counter("hermes.foo.bar", ...) and friends.
INSTRUMENT = re.compile(
    r"\.(?:Int64|Float64)(?:Counter|UpDownCounter|Histogram|Gauge|"
    r"ObservableCounter|ObservableUpDownCounter|ObservableGauge)\(\s*\"([^\"]+)\""
)

# Any hermes_-prefixed identifier appearing in a query. Recording rules define their own
# names with a colon (hermes:consumer_pending), which are not instruments.
#
# The trailing `[a-z0-9]` before the word boundary is what keeps prose out. Comments in
# the rules files refer to metric families as `hermes_messaging_*`, and a pattern ending
# in `_+` matches the stem of that glob as though it were a metric name — which fails the
# build over a sentence.
REFERENCE = re.compile(r"\bhermes_[a-z0-9_]*[a-z0-9]\b")

# Suffixes the Prometheus exporter appends. Stripped longest-first so that
# _duration_seconds_bucket reduces past _bucket and then past _seconds.
SUFFIXES = (
    "_bucket",
    "_count",
    "_sum",
    "_total",
    "_seconds",
    "_bytes",
    "_ratio",
    "_milliseconds",
)


def emitted_metrics(root):
    """Prometheus-style base names for every instrument created in Go code."""
    names = set()
    for go_dir in GO_DIRS:
        path = os.path.join(root, go_dir)
        for dirpath, _, filenames in os.walk(path):
            for filename in filenames:
                if not filename.endswith(".go"):
                    continue
                full = os.path.join(dirpath, filename)
                with open(full, encoding="utf-8") as fh:
                    for otel_name in INSTRUMENT.findall(fh.read()):
                        names.add(otel_name.replace(".", "_"))
    return names


def strip_suffixes(name):
    """Every prefix of `name` reachable by removing exporter-appended suffixes."""
    seen = {name}
    changed = True
    while changed:
        changed = False
        for candidate in list(seen):
            for suffix in SUFFIXES:
                if candidate.endswith(suffix):
                    shorter = candidate[: -len(suffix)]
                    if shorter and shorter not in seen:
                        seen.add(shorter)
                        changed = True
    return seen


def referenced_metrics(root):
    """{metric name: {files referencing it}} across dashboards and rules."""
    refs = {}
    for query_dir in QUERY_DIRS:
        path = os.path.join(root, query_dir)
        if not os.path.isdir(path):
            continue
        for dirpath, _, filenames in os.walk(path):
            for filename in filenames:
                if not filename.endswith((".json", ".yaml", ".yml")):
                    continue
                full = os.path.join(dirpath, filename)
                rel = os.path.relpath(full, root)
                with open(full, encoding="utf-8") as fh:
                    for name in REFERENCE.findall(fh.read()):
                        refs.setdefault(name, set()).add(rel)
    return refs


def check(emitted, referenced):
    """Referenced names that reduce to no emitted instrument."""
    failures = []
    for name in sorted(referenced):
        if any(candidate in emitted for candidate in strip_suffixes(name)):
            continue
        where = ", ".join(sorted(referenced[name]))
        failures.append(f"{name} is queried in {where} but no Go file emits it")
    return failures


def unread(emitted, referenced):
    """Instruments no query mentions. Advisory."""
    reduced = set()
    for name in referenced:
        reduced |= strip_suffixes(name)
    return sorted(name for name in emitted if name not in reduced)


def main(argv):
    root = "."
    for arg in argv:
        if arg.startswith("--root="):
            root = arg.split("=", 1)[1]

    emitted = emitted_metrics(root)
    if not emitted:
        # A gate that reads nothing and reports success is worse than no gate.
        print("ERROR: found no metric instruments in Go source", file=sys.stderr)
        return 1

    referenced = referenced_metrics(root)
    if not referenced:
        print("ERROR: found no metric references in dashboards or rules", file=sys.stderr)
        return 1

    for name in unread(emitted, referenced):
        print(f"note: {name} is emitted but no dashboard or alert reads it", file=sys.stderr)

    failures = check(emitted, referenced)
    if failures:
        print(f"FAIL: {len(failures)} queries reference metrics that are not emitted:\n", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        print(
            "\nA panel with no series looks exactly like a healthy service with no traffic,\n"
            "and an alert over a metric that does not exist never fires. Add the instrument,\n"
            "or fix the query. See docs/observability/metrics-reference.md.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: {len(referenced)} metric references, all emitted by Go code")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
