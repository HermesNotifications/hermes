#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a PodDisruptionBudget protects nothing, or can never allow a disruption.

The direct sibling of check_networkpolicy_selectors.py, and the same defect class: a control
that renders, applies, and does nothing.

Finding 36's verification introduced a one-character typo into the `hermes-send` PDB's
selector — `hermes-sned` — and watched `expectedPods` drop from 3 to 0 **with no error from
kustomize, from kubectl, or from the API server**. A PodDisruptionBudget that selects no pods
is accepted everywhere and reports itself as fine. It simply stops protecting the workload,
and the first evidence is a node drain taking every replica of the ingestion path at once.

Three shapes are rejected.

  * **A selector matching no workload.** The typo case above. `expectedPods: 0`.

  * **A selector matching only Job or CronJob pods.** `deploy/k8s/overlays/production/pdb/README.md`
    lists hermes-migrate, hermes-natsprovision and the cleanup CronJob as deliberately
    uncovered, because an evicted Job pod is never replaced — so the budget can never be
    satisfied again and the drain waits forever. That "deliberately" was prose; this makes it
    enforceable.

  * **A budget that pins `disruptionsAllowed: 0` permanently**, i.e. integer `minAvailable`
    greater than or equal to the matched workloads' total replicas, or `minAvailable: 100%`.
    The README names this as the classic footgun and says it does not arise today "but it
    would the moment someone adds a workload at `replicas: 1`". Nothing enforced that either,
    and it is worse than an inert budget: it blocks the drain that would fix the situation,
    which is exactly the wedge finding 36 reproduced on the NATS PDB below quorum.

Kubernetes label-selector semantics are **imported** from check_networkpolicy_selectors rather
than reimplemented. Two copies drift, and the drift is invisible — each gate goes on passing
its own tests while disagreeing about the same manifest.

Not applied to the `local` overlay, which renders no PodDisruptionBudgets at all (nor HPAs,
nor Jobs): it is a laptop environment with no node drains to survive. Staging renders none
either today; production renders eleven. The gate is pointed at production, and its
zero-sentinel means "staging grew PDBs and nobody pointed the gate at them" stays visible as
a deliberate Makefile change rather than a silent gap.

Usage:
    check_pdb_selectors.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_pdb_selectors.py -
"""

import sys

from check_networkpolicy_selectors import pod_template_labels, selects

# Kinds whose pods a PDB can meaningfully protect: a controller replaces an evicted pod, so
# the budget can recover. Job/CronJob pods are tracked separately — they are pod-producing,
# so they can satisfy a selector, but they can never satisfy a *budget*.
REPLACING_KINDS = ("Deployment", "StatefulSet", "ReplicaSet", "DaemonSet")
TERMINATING_KINDS = ("Job", "CronJob")


def workload_records(docs):
    """(name, kind, pod labels, replicas) for every document that creates a pod.

    Richer than check_networkpolicy_selectors.workloads() because the budget arithmetic needs
    the replica count. `spec.replicas` defaults to 1 when omitted, exactly as Kubernetes
    does — a Deployment with no replicas field is a one-replica Deployment, and a
    `minAvailable: 1` budget over it is wedged just as surely as the explicit case.
    """
    out = []
    for doc in docs:
        kind = doc.get("kind")
        name = (doc.get("metadata") or {}).get("name", "?")
        spec = doc.get("spec") or {}
        if kind in REPLACING_KINDS:
            replicas = spec.get("replicas")
            out.append((name, kind, pod_template_labels(spec), 1 if replicas is None else replicas))
        elif kind == "Job":
            out.append((name, kind, pod_template_labels(spec), spec.get("parallelism") or 1))
        elif kind == "CronJob":
            job_spec = (spec.get("jobTemplate") or {}).get("spec") or {}
            out.append((name, kind, pod_template_labels(job_spec), 1))
    return out


def blocked_reason(spec, matched):
    """Why this budget can never allow a disruption, or None if it can.

    Only `minAvailable` can wedge a drain. `maxUnavailable` is expressed as a ceiling on
    disruptions rather than a floor on survivors, so it always permits at least one eviction
    at any replica count — which is why the production PDBs were moved to it (see README).
    """
    min_available = spec.get("minAvailable")
    if min_available is None:
        return None

    total = sum(replicas for _, _, _, replicas in matched)

    if isinstance(min_available, str):
        if not min_available.endswith("%"):
            return None
        try:
            percent = float(min_available[:-1])
        except ValueError:
            return f"minAvailable: {min_available!r} is not a valid percentage"
        if percent >= 100:
            return (
                f"minAvailable: {min_available} leaves no room for a single eviction, so "
                "disruptionsAllowed: 0 permanently and node drains block forever"
            )
        return None

    if min_available >= total:
        return (
            f"minAvailable: {min_available} against {total} replica(s) across the workloads "
            "it matches, so disruptionsAllowed: 0 permanently — the budget blocks every node "
            "drain instead of pacing it"
        )
    return None


def evaluate(docs):
    """Return (failures, PDBs checked, workloads found).

    `failures` is a list of (PDB name, reason). Split out from main() so the gate's decision
    is testable without rendering YAML, as the other gates in this directory do it.
    """
    pods = workload_records(docs)
    failures = []
    checked = 0

    for doc in docs:
        if doc.get("kind") != "PodDisruptionBudget":
            continue
        checked += 1
        name = (doc.get("metadata") or {}).get("name", "?")
        spec = doc.get("spec") or {}
        selector = spec.get("selector")

        matched = [w for w in pods if selects(selector, w[2])]
        if not matched:
            failures.append((
                name,
                f"selector matches none of the {len(pods)} workloads; expectedPods would be 0 "
                "and the budget protects nothing",
            ))
            continue

        replacing = [w for w in matched if w[1] in REPLACING_KINDS]
        if not replacing:
            kinds = ", ".join(sorted({f"{w[1]}/{w[0]}" for w in matched}))
            failures.append((
                name,
                f"matches only pods that are never replaced ({kinds}); an evicted Job pod "
                "does not come back, so the budget can never be satisfied and the drain hangs",
            ))
            continue

        reason = blocked_reason(spec, replacing)
        if reason:
            failures.append((name, reason))

    return failures, checked, len(pods)


def report(failures, checked, pod_count):
    """Print the verdict and return the exit code. Separated so the sentinels are testable."""
    # Either of these means the gate silently verified nothing, which is the failure mode it
    # exists to prevent — so they are errors, not passes.
    if pod_count == 0:
        print("ERROR: no pod-producing workloads found; is this the right manifest?", file=sys.stderr)
        return 1
    if checked == 0:
        print(
            "ERROR: no PodDisruptionBudgets found; is this the right manifest? Only the "
            "production overlay renders them today — if that changed, point this gate at the "
            "overlay that lost them rather than removing the step.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} PodDisruptionBudgets do not protect anything:\n",
              file=sys.stderr)
        for name, reason in failures:
            print(f"  PodDisruptionBudget/{name} {reason}", file=sys.stderr)
        print(
            "\nA PDB that selects nothing is accepted by kustomize, by kubectl and by the API\n"
            "server, and reports itself as healthy — a one-character typo in the send PDB's\n"
            "selector (`hermes-sned`) took expectedPods from 3 to 0 with no error from any\n"
            "tool. A PDB that can never allow a disruption is worse: it blocks the very drain\n"
            "that would resolve the situation. See deploy/k8s/overlays/production/pdb/README.md.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} PodDisruptionBudgets protect at least one replaceable workload "
          f"of {pod_count} and can allow a disruption")
    return 0


def main(paths):
    try:
        import yaml
    except ImportError:
        for line in (
            "ERROR: PyYAML is not installed, so this gate can verify nothing.",
            "A gate that verified nothing must not report success -- the same rule these",
            "checks already apply to empty input. Install it with:",
            "    python3 -m pip install -r scripts/requirements.txt",
            "or run `make verify-manifests`, which provisions .venv from that file.",
        ):
            print(line, file=sys.stderr)
        return 1

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    return report(*evaluate(docs))


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
