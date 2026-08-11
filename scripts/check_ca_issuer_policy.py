#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a `ca` ClusterIssuer is rendered without an admission policy scoping who may use it.

ADR 0005 phase 4 moved the Hermes internal CA's private key out of the application namespace by
making the CA a ClusterIssuer. That was a deliberate trade and the record says so — but it left
a residual it explicitly did not close: a ClusterIssuer can be referenced by a Certificate in
ANY namespace, and phase 4 verified that by minting a certificate with SAN nats.hermes.svc from
a namespace that is not `hermes`. A leaf from this CA is trusted by every Hermes service, so
that is enough to impersonate the NATS server to any client.

deploy/k8s/pki/restrict-internal-ca.yaml closes it with a ValidatingAdmissionPolicy. This gate
exists because that policy has two independent ways of being present and inert, neither of which
changes anything visible in a rendered diff or a `kubectl apply`:

  * a ValidatingAdmissionPolicy with no ValidatingAdmissionPolicyBinding is never evaluated —
    the same failure class as finding 47's NetworkPolicy whose podSelector matched nothing;
  * a binding whose validationActions are Warn or Audit rather than Deny logs the violation and
    admits the request anyway.

It also fails when a `ca` ClusterIssuer is added with no policy covering it at all, which is the
case that matters most: the residual is a property of using a ClusterIssuer for a CA, so a
second one added later inherits the same hole and should not be able to arrive quietly.

`failurePolicy: Ignore` is rejected for the same reason the policy sets Fail: an admission
control that fails open when it cannot evaluate is the shape of defect ADR 0005 exists to
remove.

Usage:
    check_ca_issuer_policy.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_ca_issuer_policy.py -
"""

import sys

CERT_MANAGER_GROUP = "cert-manager.io"
# The two request kinds a policy must cover. A CertificateRequest can be created directly,
# referencing an issuer with no Certificate object at all, so policing only Certificate leaves
# the shorter path open.
REQUIRED_RESOURCES = {"certificates", "certificaterequests"}


def _policy_covers_cert_manager_requests(policy):
    """True if this policy's matchConstraints select both cert-manager request kinds."""
    spec = policy.get("spec") or {}
    covered = set()
    for rule in (spec.get("matchConstraints") or {}).get("resourceRules") or []:
        if CERT_MANAGER_GROUP not in (rule.get("apiGroups") or []):
            continue
        operations = rule.get("operations") or []
        if "CREATE" not in operations and "*" not in operations:
            continue
        for resource in rule.get("resources") or []:
            covered.add(resource)
    if "*" in covered:
        return True
    return REQUIRED_RESOURCES.issubset(covered)


def _mentions_issuer(policy, issuer_name):
    """True if any matchCondition or validation expression names this issuer.

    Deliberately a substring test over the CEL rather than a parse of it. The gate's job is to
    catch an ABSENT or INERT policy, not to re-implement CEL and pass judgement on a present
    one — a policy that names the issuer somewhere in its expressions is taken at its word.
    """
    spec = policy.get("spec") or {}
    expressions = [
        condition.get("expression", "")
        for condition in (spec.get("matchConditions") or [])
    ] + [
        validation.get("expression", "")
        for validation in (spec.get("validations") or [])
    ]
    return any(issuer_name in expression for expression in expressions)


def evaluate(docs):
    """Return (failures, ca ClusterIssuers checked).

    Split out from main() so the gate's decision is testable without rendering YAML, the same
    way check_ca_key_location.py does it.
    """
    ca_issuers = [
        doc
        for doc in docs
        if doc.get("kind") == "ClusterIssuer" and "ca" in (doc.get("spec") or {})
    ]
    policies = {
        (doc.get("metadata") or {}).get("name"): doc
        for doc in docs
        if doc.get("kind") == "ValidatingAdmissionPolicy"
    }
    bindings = [doc for doc in docs if doc.get("kind") == "ValidatingAdmissionPolicyBinding"]

    failures = []

    for issuer in ca_issuers:
        name = (issuer.get("metadata") or {}).get("name", "?")

        covering = [
            policy
            for policy in policies.values()
            if _policy_covers_cert_manager_requests(policy) and _mentions_issuer(policy, name)
        ]
        if not covering:
            failures.append(
                f"ClusterIssuer/{name} is a `ca` issuer usable from any namespace, and no "
                "ValidatingAdmissionPolicy covering cert-manager Certificates and "
                "CertificateRequests names it"
            )
            continue

        for policy in covering:
            policy_name = (policy.get("metadata") or {}).get("name", "?")

            if ((policy.get("spec") or {}).get("failurePolicy") or "Fail") != "Fail":
                failures.append(
                    f"ValidatingAdmissionPolicy/{policy_name} (guarding ClusterIssuer/{name}) "
                    "sets failurePolicy: Ignore; it would admit requests it cannot evaluate"
                )

            bound = [
                binding
                for binding in bindings
                if ((binding.get("spec") or {}).get("policyName")) == policy_name
            ]
            if not bound:
                failures.append(
                    f"ValidatingAdmissionPolicy/{policy_name} (guarding ClusterIssuer/{name}) "
                    "has no ValidatingAdmissionPolicyBinding; an unbound policy is never "
                    "evaluated"
                )
                continue

            if not any(
                "Deny" in ((binding.get("spec") or {}).get("validationActions") or [])
                for binding in bound
            ):
                failures.append(
                    f"ValidatingAdmissionPolicy/{policy_name} (guarding ClusterIssuer/{name}) "
                    "is bound only with Warn/Audit actions; violations would be logged and "
                    "admitted"
                )

    return failures, len(ca_issuers)


def main(argv):
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
    for path in argv:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    failures, checked = evaluate(docs)

    # A gate that verified nothing must not report success.
    if checked == 0:
        print(
            "ERROR: no `ca` ClusterIssuer found; is this the right manifest?",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(
            f"FAIL: a CA ClusterIssuer is usable from any namespace ({len(failures)} problem(s) "
            f"across {checked} `ca` ClusterIssuer(s)):\n",
            file=sys.stderr,
        )
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            "\nA leaf signed by this CA is trusted by every Hermes service, so any namespace\n"
            "able to request one can impersonate the NATS server (ADR 0005 phase 4's named\n"
            "residual). See deploy/k8s/pki/restrict-internal-ca.yaml.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: every `ca` ClusterIssuer is scoped by a denying admission policy ({checked} checked)")
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
