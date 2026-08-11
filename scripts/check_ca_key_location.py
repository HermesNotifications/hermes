#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if the internal CA's private key would land in the application namespace.

ADR 0005 phase 4. Phase 2 issued Hermes' internal certificates from a namespaced `Issuer`
of type `ca` in `hermes`, which put the CA private key in a Secret in the application
namespace — so anything able to read Secrets there could mint a certificate every Hermes
service trusts. Phase 4 moved the CA to a `ClusterIssuer`, whose signing Secret cert-manager
resolves in its own cluster-resource namespace instead.

That fix rests entirely on one line of YAML: `namespace: cert-manager` on the CA Certificate,
and the fact that deploy/k8s/pki is referenced as a sibling of base rather than from inside
it. base/kustomization.yaml sets `namespace: hermes`, and the kustomize namespace transformer
rewrites the namespace of every namespaced resource it covers. Move that file under base/, or
add a `namespace:` to an overlay, and the CA private key silently returns to `hermes` — with
a rendered manifest that applies cleanly, certificates that still issue, and NATS still
serving TLS. Nothing about the failure is visible in behaviour, which is why it needs a gate.

Two shapes are rejected:

  * a Certificate with `isCA: true` in the application namespace — its secretName holds the
    CA key, and cert-manager writes that Secret next to the Certificate;
  * a namespaced `Issuer` with a `ca` stanza in the application namespace — a namespaced
    issuer can only read its signing Secret from its own namespace, so its presence means
    the key is expected to be there, whether or not a Certificate is rendered alongside it.

A `selfSigned` Issuer is not flagged: it holds no key.

Usage:
    check_ca_key_location.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_ca_key_location.py - --namespace hermes
"""

import sys

DEFAULT_APP_NAMESPACE = "hermes"


def evaluate(docs, app_namespace):
    """Return (failures, certificates-and-issuers checked).

    Split out from main() so the gate's decision is testable without rendering YAML, the
    same way check_networkpolicy_selectors.py does it.
    """
    failures = []
    checked = 0

    for doc in docs:
        kind = doc.get("kind")
        if kind not in ("Certificate", "Issuer"):
            continue
        meta = doc.get("metadata") or {}
        spec = doc.get("spec") or {}
        name = meta.get("name", "?")
        namespace = meta.get("namespace")
        checked += 1

        if namespace != app_namespace:
            continue

        if kind == "Certificate" and spec.get("isCA"):
            failures.append(
                f"Certificate/{name} has isCA: true in namespace {namespace}; "
                f"its key Secret ({spec.get('secretName', '?')}) would live in the "
                "application namespace"
            )
        elif kind == "Issuer" and "ca" in spec:
            failures.append(
                f"Issuer/{name} is a namespaced `ca` issuer in namespace {namespace}; "
                f"it can only read its signing Secret ({(spec.get('ca') or {}).get('secretName', '?')}) "
                "from that namespace"
            )

    return failures, checked


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

    paths = []
    app_namespace = DEFAULT_APP_NAMESPACE
    it = iter(argv)
    for arg in it:
        if arg == "--namespace":
            app_namespace = next(it)
        else:
            paths.append(arg)

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    failures, checked = evaluate(docs, app_namespace)

    # A gate that verified nothing must not report success.
    if checked == 0:
        print(
            "ERROR: no Certificate or Issuer resources found; is this the right manifest?",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(
            f"FAIL: the internal CA private key would be readable in the {app_namespace} "
            f"namespace ({len(failures)} of {checked} PKI resources):\n",
            file=sys.stderr,
        )
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            "\nAnything that can read Secrets in the application namespace could then mint a\n"
            "certificate every Hermes service trusts (ADR 0005 phase 4). The CA belongs in a\n"
            "ClusterIssuer whose Secret lives in cert-manager's cluster-resource namespace —\n"
            "see deploy/k8s/pki/. The usual cause is deploy/k8s/pki being reached from inside\n"
            "base/, where `namespace: hermes` rewrites the CA Certificate's namespace.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: no CA private key in the {app_namespace} namespace ({checked} PKI resources checked)")
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
