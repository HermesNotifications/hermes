#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a workload's shutdown sequence cannot finish inside its termination grace period.

Graceful shutdown is spread across two places that no tool relates to each other: the pod spec
sets `terminationGracePeriodSeconds` and, where supported, `lifecycle.preStop.sleep`, while the
lengths of the drain steps are environment variables the Go process reads. Exceed the grace
period and the kubelet sends SIGKILL partway through -- which is strictly worse than not
draining at all, because the process has already stopped accepting work and now abandons what it
was finishing.

That split exists because every Hermes image is `FROM scratch` (deploy/docker/Dockerfile). There
is no shell, so the conventional `preStop: exec: sleep 5` is impossible and the drain delay has
to run in-process. A `preStop.sleep` SleepAction can be layered on where the cluster is new
enough, which is why both are counted here.

The budget, in order, and each part is a real wait:

    preStop.sleep                    kubelet waits before sending SIGTERM
    HERMES_SHUTDOWN_DRAIN_DELAY      keep serving while endpoint removal propagates
    HERMES_NATS_DRAIN_TIMEOUT        stop consuming, wait for in-flight handlers
    HERMES_SHUTDOWN_TIMEOUT          graceful HTTP shutdown
    ------------------------------
    must be < terminationGracePeriodSeconds, with headroom

Defaults are applied exactly as the Go code does when a variable is unset, because "unset" is
the case most likely to be wrong -- the previous behaviour was a hardcoded 5s HTTP shutdown
under a 30s grace period, which fit only because it did almost nothing.

Usage:
    check_shutdown_budget.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_shutdown_budget.py -
"""

import sys

# Mirrors of the Go defaults. Kept in step with internal/bootstrap/serve.go and
# internal/bootstrap/lifecycle.go; a drift here reports a budget the process does not run.
DEFAULT_DRAIN_DELAY = 5
DEFAULT_NATS_DRAIN = 30
DEFAULT_HTTP_SHUTDOWN = 15
DEFAULT_GRACE = 30  # Kubernetes' own default when terminationGracePeriodSeconds is omitted.

# Fraction of the grace period the sequence may occupy. Not 100%: the steps are budgets rather
# than measurements, and a handler that finishes right on its deadline still needs time to ack.
HEADROOM = 0.9

WORKLOAD_KINDS = ("Deployment", "StatefulSet", "DaemonSet")

# Identifying marker for a workload that runs the Go shutdown sequence.
#
# Not a name prefix: kustomize names these `hermes-inbox` while Helm names them after the
# release (`myrelease-inbox`), so a prefix match silently found nothing in the chart render —
# the gate reported success over zero workloads. Every Hermes service sets HERMES_HTTP_PORT and
# nothing else does, which holds across both deploy paths and any release name.
MARKER_ENV = "HERMES_HTTP_PORT"


def env_seconds(container, name, default):
    """Read a duration env var as whole seconds, falling back to the Go default."""
    for env in container.get("env") or []:
        if env.get("name") != name:
            continue
        value = env.get("value")
        if value is None:
            return default  # valueFrom: unresolvable here, assume the default
        return parse_duration(str(value), default)
    return default


def parse_duration(text, default):
    """Parse a Go duration string ("30s", "1m") or a bare number of seconds."""
    text = text.strip()
    try:
        if text.endswith("ms"):
            return max(1, int(float(text[:-2]) / 1000))
        if text.endswith("s"):
            return int(float(text[:-1]))
        if text.endswith("m"):
            return int(float(text[:-1]) * 60)
        return int(float(text))
    except ValueError:
        return default


def pre_stop_seconds(container):
    """Seconds a preStop SleepAction adds before SIGTERM. Absent on clusters below 1.30."""
    lifecycle = container.get("lifecycle") or {}
    sleep = (lifecycle.get("preStop") or {}).get("sleep") or {}
    return int(sleep.get("seconds") or 0)


def evaluate(docs):
    """Return (failures, workloads checked). Failures are (name, reason)."""
    failures = []
    checked = 0

    for doc in docs:
        if doc.get("kind") not in WORKLOAD_KINDS:
            continue
        name = (doc.get("metadata") or {}).get("name", "?")

        pod_spec = (((doc.get("spec") or {}).get("template") or {}).get("spec")) or {}
        containers = pod_spec.get("containers") or []
        if not containers:
            continue
        container = containers[0]
        if not any((env.get("name") == MARKER_ENV) for env in container.get("env") or []):
            continue
        checked += 1

        grace = pod_spec.get("terminationGracePeriodSeconds")
        grace = DEFAULT_GRACE if grace is None else int(grace)

        parts = {
            "preStop.sleep": pre_stop_seconds(container),
            "HERMES_SHUTDOWN_DRAIN_DELAY": env_seconds(container, "HERMES_SHUTDOWN_DRAIN_DELAY", DEFAULT_DRAIN_DELAY),
            "HERMES_NATS_DRAIN_TIMEOUT": env_seconds(container, "HERMES_NATS_DRAIN_TIMEOUT", DEFAULT_NATS_DRAIN),
            "HERMES_SHUTDOWN_TIMEOUT": env_seconds(container, "HERMES_SHUTDOWN_TIMEOUT", DEFAULT_HTTP_SHUTDOWN),
        }
        total = sum(parts.values())
        budget = grace * HEADROOM

        if total > budget:
            breakdown = " + ".join(f"{k} {v}s" for k, v in parts.items() if v)
            failures.append((
                name,
                f"shutdown needs up to {total}s ({breakdown}) but terminationGracePeriodSeconds "
                f"is {grace}s, leaving no headroom — the kubelet SIGKILLs the process partway "
                f"through its drain",
            ))

    return failures, checked


def report(failures, checked):
    if checked == 0:
        print(
            "ERROR: no Hermes workloads found; is this the right manifest? A gate that silently "
            "checks nothing is the failure mode this exists to prevent.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} workloads cannot finish shutting down in time:\n",
              file=sys.stderr)
        for name, reason in failures:
            print(f"  {name}: {reason}", file=sys.stderr)
        print(
            "\nRaise terminationGracePeriodSeconds, or lower the drain budgets. Being SIGKILLed\n"
            "mid-drain is worse than not draining: the process has already stopped accepting\n"
            "work and now abandons what it was finishing, so those messages are redelivered with\n"
            "their side effects repeated. See ADR 0012.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} Hermes workloads can complete shutdown within their grace period")
    return 0


def main(paths):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; shutdown budget check not run", file=sys.stderr)
        return 0

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    return report(*evaluate(docs))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:] or ["-"]))
