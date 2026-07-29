#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if any NetworkPolicy selects no pods.

This exists because of finding 47 in the 2026-07-27 architecture review. The kustomize
`labels:` transformer ran with `includeSelectors: false` and no `includeTemplates`, so
`app.kubernetes.io/part-of` was written to resource metadata but never onto a pod
template. Four of the seven NetworkPolicies keyed their podSelector on that label and
therefore selected ZERO pods — the declared effect of the entire policy set was DNS
egress and nothing else.

Nothing detected it, and nothing could have. `kustomize build` succeeds, `kubectl apply`
succeeds, and the API server happily accepts a NetworkPolicy that matches nothing. A
policy selecting nothing is indistinguishable from a policy that works, right up until
enforcement is switched on and the namespace goes dark. That is the class of defect this
closes: not a wrong rule, an inert one.

Usage:
    check_networkpolicy_selectors.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_networkpolicy_selectors.py -
"""

import sys

POD_PRODUCING_KINDS = ("Deployment", "StatefulSet", "DaemonSet", "Job")


def pod_template_labels(spec):
    """Labels on a workload's pod template, tolerating an absent metadata block.

    The tolerance is not defensive padding: before the fix, rendering produced pod
    templates with no metadata key at all, and reading it naively raised KeyError.
    """
    template = spec.get("template") or {}
    return (template.get("metadata") or {}).get("labels") or {}


def workloads(docs):
    """(name, kind, pod labels) for every document that ultimately creates a pod."""
    out = []
    for doc in docs:
        kind = doc.get("kind")
        name = (doc.get("metadata") or {}).get("name", "?")
        spec = doc.get("spec") or {}
        if kind in POD_PRODUCING_KINDS:
            out.append((name, kind, pod_template_labels(spec)))
        elif kind == "CronJob":
            job_spec = (spec.get("jobTemplate") or {}).get("spec") or {}
            out.append((name, kind, pod_template_labels(job_spec)))
    return out


def selects(selector, labels):
    """Whether a LabelSelector matches a pod carrying `labels`.

    Mirrors Kubernetes semantics closely enough to answer the only question asked here:
    does this select anything at all. An empty selector matches every pod; a missing one
    matches nothing.
    """
    if selector is None:
        return False
    if selector == {}:
        return True

    for key, value in (selector.get("matchLabels") or {}).items():
        if labels.get(key) != value:
            return False

    for expr in selector.get("matchExpressions") or []:
        key = expr.get("key")
        operator = expr.get("operator")
        values = expr.get("values") or []
        present = key in labels
        if operator == "In" and labels.get(key) not in values:
            return False
        if operator == "NotIn" and labels.get(key) in values:
            return False
        if operator == "Exists" and not present:
            return False
        if operator == "DoesNotExist" and present:
            return False

    return True


def evaluate(docs):
    """Return (failures, policies checked, workloads found).

    `failures` is a list of (policy name, workload count) for policies matching nothing.
    Split out from main() so the gate's decision is testable without rendering YAML.
    """
    pods = workloads(docs)
    failures = []
    checked = 0

    for doc in docs:
        if doc.get("kind") != "NetworkPolicy":
            continue
        checked += 1
        name = (doc.get("metadata") or {}).get("name", "?")
        selector = (doc.get("spec") or {}).get("podSelector")
        if not any(selects(selector, labels) for _, _, labels in pods):
            failures.append((name, len(pods)))

    return failures, checked, len(pods)


def main(paths):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; NetworkPolicy selector check not run", file=sys.stderr)
        return 0

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    failures, checked, pod_count = evaluate(docs)

    # Both of these mean the check silently verified nothing, which is the very failure
    # mode it exists to prevent — so they are errors, not passes.
    if pod_count == 0:
        print("ERROR: no pod-producing workloads found; is this the right manifest?", file=sys.stderr)
        return 1
    if checked == 0:
        print("ERROR: no NetworkPolicies found; is this the right manifest?", file=sys.stderr)
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} NetworkPolicies select no pods:\n", file=sys.stderr)
        for name, total in failures:
            print(f"  {name}: podSelector matches none of the {total} workloads", file=sys.stderr)
        print(
            "\nA policy that selects nothing is silently inert: it neither permits nor denies,\n"
            "and it looks identical to one that works until enforcement is enabled.\n"
            "Usually a label the podSelector keys on is not reaching pod templates — check\n"
            "`labels:` in deploy/k8s/base/kustomization.yaml (includeTemplates must be true).",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} NetworkPolicies select at least one of {pod_count} workloads")
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
