#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if a workload has no resource requests, or an HPA can never read its metric.

Finding 8's capacity half. `hermes-send` — the service every write passes through — was
missing from `patches/replicas.yaml`, `patches/resources.yaml`, `patches/anti-affinity.yaml`,
the HPA set and the PDB set. Five independent omissions, in the overlay that matters most,
and `kubectl kustomize` was perfectly happy with every one of them. In production it would
have run as one pod with no requests, no autoscaling, no disruption budget and no spread.

Three things are checked.

  1. **Every Deployment and StatefulSet container declares cpu and memory requests and a
     memory limit.** Without requests the scheduler packs the node blind and the pod is the
     first thing evicted under pressure; without a memory limit a leak takes the node rather
     than the pod.

     A CPU *limit* is deliberately not required. `patches/resources.yaml` argues the case
     directly — "a CPU limit on the ingestion path converts a burst into throttling and
     queueing rather than into a scale-up" — and a gate demanding one would force a change
     the overlay rejects on the merits.

  2. **Every HPA's `scaleTargetRef` names a workload of that kind and name in the same
     render.** A dangling reference applies cleanly and sits at `FailedGetScale` forever.

  3. **Every container of an HPA's target declares a request for each resource the HPA
     measures.** This is the one with teeth, and it pins a measured failure rather than a
     theory: removing send's requests flipped its HPA to
     `ScalingActive=False, FailedGetResourceMetric` against a real metrics-server. The HPA
     and the resources patch are coupled — an HPA over a target with no CPU request exists,
     reports healthy-ish, and silently never scales — and until now nothing expressed that
     coupling anywhere. The metric list is read rather than assumed to be CPU, and *every*
     container must carry the request, because the HPA sums across containers and one
     missing request makes the whole metric unavailable rather than merely less accurate.

Jobs and CronJobs are not counted. They are not scaled, not long-lived, and not disrupted;
`hermes-migrate` renders with no resources at all today and that is a separate question from
this one.

**The `local` overlay is deliberately not held to this standard**, and is exempted here in
writing rather than silently filtered. It renders `postgres`, `redis`, `mailpit` and
`dynamodb-local` with no resources at all — laptop conveniences that reach no cluster — and
it renders zero HPAs and zero PDBs, so there is no autoscaling to disable and no drain to
survive. The alternative, running the gate on local while skipping non-`hermes-*` workloads,
was considered and rejected: a special-case filter is exactly the mechanism that hides the
next omission. Staging and production render the full set and are held to all three checks.

Usage:
    check_workload_resources.py rendered.yaml [...] [--require-hpa]
    kubectl kustomize deploy/k8s/overlays/production | check_workload_resources.py - --require-hpa
"""

import sys

WORKLOAD_KINDS = ("Deployment", "StatefulSet")


def containers_of(doc):
    """(section, container) for every container in a workload's pod template.

    initContainers are included for the requests check. None render today; checking them is
    cheaper than a comment promising to later.
    """
    pod = ((doc.get("spec") or {}).get("template") or {}).get("spec") or {}
    for name in ("initContainers", "containers"):
        for container in pod.get(name) or []:
            yield name, container


def missing_resources(container):
    """Which of the three required resource fields this container omits."""
    resources = container.get("resources") or {}
    requests = resources.get("requests") or {}
    limits = resources.get("limits") or {}

    missing = []
    if not requests.get("cpu"):
        missing.append("cpu request")
    if not requests.get("memory"):
        missing.append("memory request")
    if not limits.get("memory"):
        missing.append("memory limit")
    return missing


def metric_resources(hpa_spec):
    """Resource names an HPA reads via `type: Resource` metrics (typically cpu, memory).

    Other metric types — External, Pods, Object — have nothing to do with the target's
    resource requests, so they are not a reason to demand one.
    """
    names = []
    for metric in hpa_spec.get("metrics") or []:
        if metric.get("type") != "Resource":
            continue
        name = (metric.get("resource") or {}).get("name")
        if name:
            names.append(name)
    return names


def evaluate(docs, skip_names=()):
    """Return (failures, workloads checked, HPAs checked).

    `failures` is a list of (subject, reason), where subject is `Kind/name`. Split out from
    main() so the gate's decision is testable without rendering YAML, as the other gates in
    this directory do it.

    `skip_names` exempts workloads whose name contains any of the given substrings. It exists
    for one case: the Helm chart's bundled sub-charts (NATS, Centrifugo, and their sidecars)
    are third-party workloads whose resources are governed by those charts' own values, and
    charts/hermes/templates/_validate.tpl refuses to render them at all outside development —
    so they never reach a production cluster. The kustomize overlays, which are the production
    path, render none of them and pass no exemptions.
    """
    failures = []
    workloads = {}

    for doc in docs:
        if doc.get("kind") not in WORKLOAD_KINDS:
            continue
        name = (doc.get("metadata") or {}).get("name", "?")
        if any(skip in name for skip in skip_names):
            continue
        workloads[(doc["kind"], name)] = doc

    for (kind, name), doc in workloads.items():
        for section, container in containers_of(doc):
            missing = missing_resources(container)
            if missing:
                label = container.get("name", "?")
                where = " initContainer" if section == "initContainers" else " container"
                failures.append((
                    f"{kind}/{name}",
                    f"{where} {label!r} has no {', no '.join(missing)}",
                ))

    hpas = 0
    for doc in docs:
        if doc.get("kind") != "HorizontalPodAutoscaler":
            continue
        hpas += 1
        name = (doc.get("metadata") or {}).get("name", "?")
        spec = doc.get("spec") or {}
        ref = spec.get("scaleTargetRef") or {}
        subject = f"HorizontalPodAutoscaler/{name}"

        target = workloads.get((ref.get("kind"), ref.get("name")))
        if target is None:
            failures.append((
                subject,
                f"scaleTargetRef names {ref.get('kind')}/{ref.get('name')}, which this render "
                "does not contain; the HPA applies cleanly and sits at FailedGetScale forever",
            ))
            # One clear error beats two that say the same thing — the coupling check below
            # would only restate that the target is missing.
            continue

        for resource in metric_resources(spec):
            lacking = [
                c.get("name", "?")
                # Containers only: the HPA sums resource metrics over containers and does not
                # see initContainers.
                for c in ((target.get("spec") or {}).get("template") or {}).get("spec", {}).get("containers") or []
                if not ((c.get("resources") or {}).get("requests") or {}).get(resource)
            ]
            if lacking:
                failures.append((
                    subject,
                    f"measures {resource} utilisation, but container(s) {', '.join(lacking)} of "
                    f"{ref.get('kind')}/{ref.get('name')} declare no {resource} request — the HPA "
                    "reports ScalingActive=False, FailedGetResourceMetric and never scales",
                ))

    return failures, len(workloads), hpas


def report(failures, workloads, hpas, require_hpa):
    """Print the verdict and return the exit code. Separated so the sentinels are testable."""
    # A gate that checked nothing must not report success.
    if workloads == 0:
        print("ERROR: no Deployments or StatefulSets found; is this the right manifest?",
              file=sys.stderr)
        return 1
    if require_hpa and hpas == 0:
        print(
            "ERROR: --require-hpa was given and this render contains no HorizontalPodAutoscalers. "
            "Production is the only overlay with autoscaling; an empty HPA set there is "
            "finding 8's defect at full size.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} resource/autoscaling problems across {workloads} workloads "
              f"and {hpas} HPAs:\n", file=sys.stderr)
        for subject, reason in failures:
            print(f"  {subject} {reason}", file=sys.stderr)
        print(
            "\nkustomize renders all of this without complaint, which is how hermes-send reached\n"
            "the production overlay missing from the replicas, resources and anti-affinity\n"
            "patches and from both the HPA and PDB sets. An HPA whose target declares no request\n"
            "for the resource it measures is the quietest of these: it reports\n"
            "ScalingActive=False / FailedGetResourceMetric and never scales, while looking like\n"
            "a configured autoscaler in every manifest and dashboard.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: {workloads} workloads declare requests and a memory limit; "
          f"{hpas} HPAs resolve their target and can read their metric")
    return 0


def main(argv):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; workload resource check not run", file=sys.stderr)
        return 0

    paths = []
    require_hpa = False
    skip_names = []
    for arg in argv:
        if arg == "--require-hpa":
            require_hpa = True
        elif arg.startswith("--skip="):
            skip_names.extend(part for part in arg.split("=", 1)[1].split(",") if part)
        else:
            paths.append(arg)

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    failures, workloads, hpas = evaluate(docs, skip_names=tuple(skip_names))
    return report(failures, workloads, hpas, require_hpa)


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
