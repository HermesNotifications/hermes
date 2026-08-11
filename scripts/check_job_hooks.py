#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a Job in the rendered overlay is not an ArgoCD hook.

`deploy/k8s/base/nats-provision-job.yaml` shipped with its hook annotations present only as
YAML comments, justified in the file itself:

    # Left commented to match hermes-migrate rather than enabling it for one Job and not
    # the other.

That claim was true when it was written and stale two commits later: PR #72 made
`hermes-migrate` a `PreSync` hook (ADR 0006), the two changes were developed in parallel, and
neither saw the other. Nothing rechecks a comment.

Why it matters. `Job.spec.template` is immutable, and
`deploy/kargo/stages/{staging,production}.yaml` rewrite the image tag on every promotion via
`kustomize-set-image`. An un-hooked Job therefore applies fine the first time and fails the
**second** promotion with

    Job.batch "hermes-natsprovision" is invalid: spec.template: Invalid value: {...}:
    field is immutable

Reproduced against ArgoCD v3.4.5. Worse than the migrate case: because the streams already
existed, the Application reported `health: Healthy` throughout while sitting at
`sync: OutOfSync`. New images rolled out, streams were never re-declared, and every dashboard
was green.

Why nothing else catches it. `kubectl kustomize` renders it, `kubectl apply` accepts it, and
`make verify-manifests` passes **identically** with the annotations present and absent — the
unit that fixed this established that by stashing its own change rather than assuming. The
failure is invisible until the second promotion of a resource that looks correct in review.

The phase is deliberately NOT pinned. The two Jobs differ for reasons that were measured:
`hermes-migrate` is `PreSync` because Aurora is external and already exists;
`hermes-natsprovision` is `Sync` because the NATS StatefulSet it provisions is created by the
same Application in the Sync phase, and both `PostSync` and a lone later `sync-wave` were
observed to deadlock outright. A gate demanding `PreSync` would have forced the broken shape.

What IS pinned is that the values are ones ArgoCD recognises. An unrecognised phase
(`presync`, `pre-sync`) is silently ignored and the Job quietly reverts to an ordinary tracked
resource — the annotation is visibly present in review and does nothing, which is precisely
the class of defect this gate exists to close.

Usage:
    check_job_hooks.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_job_hooks.py -
"""

import sys

HOOK = "argocd.argoproj.io/hook"
DELETE_POLICY = "argocd.argoproj.io/hook-delete-policy"

# https://argo-cd.readthedocs.io/en/stable/user-guide/resource_hooks/
VALID_PHASES = ("PreSync", "Sync", "PostSync", "SyncFail", "Skip")
VALID_DELETE_POLICIES = ("BeforeHookCreation", "HookSucceeded", "HookFailed")

# ADR 0006 rejected HookSucceeded on evidence: it leaves a *failed* Job in place, the next
# sync applies over it, and the original immutable-field error returns. Only a policy that
# deletes before the next creation actually removes the failure this gate exists to prevent.
REQUIRED_DELETE_POLICY = "BeforeHookCreation"


def _split(value):
    """ArgoCD accepts a comma-separated list in both annotations."""
    return [part.strip() for part in value.split(",") if part.strip()]


def check_job(name, annotations):
    """Return a reason string if this Job is not a correctly-annotated hook, else None."""
    hook = annotations.get(HOOK)
    if hook is None:
        return (
            f"has no {HOOK} annotation; Job.spec.template is immutable and Kargo rewrites "
            "the image tag on every promotion, so the second promotion fails with "
            "`field is immutable`"
        )

    phases = _split(hook)
    if not phases:
        return f"has an empty {HOOK} annotation"
    for phase in phases:
        # Exact case. ArgoCD does not normalise, it just fails to match and treats the
        # resource as an ordinary tracked one, with no warning anywhere.
        if phase not in VALID_PHASES:
            return (
                f"{HOOK}: {phase!r} is not an ArgoCD hook phase "
                f"({', '.join(VALID_PHASES)}); ArgoCD ignores an unrecognised value "
                "silently and applies the Job as an ordinary tracked resource"
            )

    policy = annotations.get(DELETE_POLICY)
    if policy is None:
        return (
            f"has {HOOK}: {hook} but no {DELETE_POLICY}; the hook alone does not remove the "
            f"previous Job, so re-applying still hits the immutable template. Set "
            f"{REQUIRED_DELETE_POLICY} (ADR 0006)"
        )

    policies = _split(policy)
    for value in policies:
        if value not in VALID_DELETE_POLICIES:
            return (
                f"{DELETE_POLICY}: {value!r} is not an ArgoCD delete policy "
                f"({', '.join(VALID_DELETE_POLICIES)})"
            )
    if REQUIRED_DELETE_POLICY not in policies:
        return (
            f"{DELETE_POLICY}: {policy!r} does not include {REQUIRED_DELETE_POLICY}. "
            "ADR 0006 rejected HookSucceeded on evidence — it leaves a failed Job in place "
            "and the next sync reproduces the immutable-field error"
        )

    return None


def evaluate(docs):
    """Return (failures, Jobs checked).

    `failures` is a list of (Job name, reason). Split out from main() so the gate's decision
    is testable without rendering YAML, the same way the other gates in this directory do it.

    CronJobs are deliberately not checked. Each run creates a fresh Job with a generated
    name, so there is no immutable template being re-applied and no reason for it to be a
    hook. Counting them would also inflate `checked` on a manifest holding no Jobs, which
    would quietly disarm the zero-sentinel in report().
    """
    failures = []
    checked = 0

    for doc in docs:
        if doc.get("kind") != "Job":
            continue
        checked += 1
        meta = doc.get("metadata") or {}
        name = meta.get("name", "?")
        reason = check_job(name, meta.get("annotations") or {})
        if reason:
            failures.append((name, reason))

    return failures, checked


def report(failures, checked):
    """Print the verdict and return the exit code. Separated so the sentinel is testable."""
    # A gate that checked nothing must not report success — that is the very shape it exists
    # to catch. Overlays with no Jobs (local) simply are not passed to this gate.
    if checked == 0:
        print(
            "ERROR: no Jobs found; is this the right manifest? This gate must be pointed at "
            "an overlay that renders Jobs (staging, production).",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} Jobs are not usable ArgoCD hooks:\n", file=sys.stderr)
        for name, reason in failures:
            print(f"  Job/{name} {reason}", file=sys.stderr)
        print(
            "\nA Job's spec.template is immutable and Kargo rewrites its image tag on every\n"
            "promotion, so an un-hooked Job applies once and then fails every promotion after\n"
            "with `field is immutable` — while the Application can still report health:\n"
            "Healthy, because nothing about the running workloads changed. See ADR 0006.\n"
            "Note the two Jobs use different phases on purpose: hermes-migrate is PreSync\n"
            "(Aurora is external), hermes-natsprovision is Sync (the bus it provisions is\n"
            "created in the same Sync phase). This gate does not pin the phase, only that it\n"
            "is one ArgoCD actually recognises.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} Jobs are ArgoCD hooks that delete before re-creation")
    return 0


def main(paths):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; Job hook check not run", file=sys.stderr)
        return 0

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
