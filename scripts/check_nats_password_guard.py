#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if NATS reads its accounts file without the guard on Centrifugo's password.

Finding 53 / ADR 0005's 2026-07-31 amendment. nats-server treats an UNSET $VARIABLE as a parse
error and refuses to start — the fail-closed behaviour nats-accounts.conf documents. An EMPTY
one is not. `HERMES_CENTRIFUGO_NATS_PASSWORD=""` parses cleanly, the server starts, the
`centrifugo` user exists with an empty password, and a client connecting as `centrifugo` with no
credential at all is accepted. Verified on the wire, not inferred; pinned by
internal/messaging/centrifugopassword_test.go.

The conf language cannot express "must be non-empty", so the guarantee is delivered by an
initContainer on the NATS StatefulSet that refuses to start the pod. That makes the guard's
presence a deployment-time property with no runtime signal: a cluster missing it comes up
looking perfectly healthy while accepting an unauthenticated Centrifugo connection. Nothing
about the failure is visible in behaviour, which is why it needs a gate.

THE INVARIANT IS CONDITIONAL, and that is the point. The local overlay legitimately has no
password: it replaces the server args to drop `-c nats.conf`, so it never reads the accounts
file and there is no `centrifugo` user to leave unauthenticated. So this does not demand the
guard everywhere — it demands the guard exactly where the server reads a config file, and it
demands its ABSENCE nowhere. What it catches:

  * the guard removed from base while staging and production still mount nats.conf;
  * the local overlay's `$patch: delete` silently ceasing to match after a rename, which would
    leave the guard in a render that has no Secret to satisfy it;
  * a new overlay that mounts the accounts file and forgets the guard entirely.

Usage:
    check_nats_password_guard.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_nats_password_guard.py -
"""

import sys

PASSWORD_VAR = "HERMES_CENTRIFUGO_NATS_PASSWORD"


def _reads_a_config_file(container):
    """True if this nats-server invocation is passed a config file.

    A server given a config file reads nats.conf, which includes nats-accounts.conf, which
    declares the `centrifugo` password user. A server started with bare `--jetstream` flags —
    the local overlay — reads neither and declares no users at all.

    All four spellings are matched, and that is not defensive padding: this gate decides
    whether the guard is REQUIRED, so a spelling it fails to recognise does not raise a false
    alarm, it silently stops requiring the guard. `nats-server --help` documents
    `-c, --config <file>`, so a manifest rewritten to the long form would turn this check off
    while leaving it green — the exact failure mode the gate exists to prevent.
    """
    tokens = [str(t) for t in (container.get("command") or []) + (container.get("args") or [])]
    return any(
        token in ("-c", "--config") or token.startswith(("-c=", "--config="))
        for token in tokens
    )


def _is_the_guard(container):
    """True if this initContainer both receives the password and inspects it.

    Both halves are required on purpose. An initContainer that merely mounts the variable
    proves nothing, and a script mentioning the name without the value being injected would
    read as unset and pass vacuously.
    """
    names = {(env or {}).get("name") for env in (container.get("env") or [])}
    if PASSWORD_VAR not in names:
        return False
    script = " ".join(
        str(part) for part in (container.get("command") or []) + (container.get("args") or [])
    )
    return PASSWORD_VAR in script


def evaluate(docs):
    """Return (failures, NATS StatefulSets checked).

    Split out from main() so the gate's decision is testable without rendering YAML, the same
    way check_ca_key_location.py does it.
    """
    failures = []
    checked = 0

    for doc in docs:
        if doc.get("kind") != "StatefulSet":
            continue
        spec = ((doc.get("spec") or {}).get("template") or {}).get("spec") or {}
        containers = spec.get("containers") or []
        servers = [c for c in containers if (c.get("image") or "").split(":")[0] == "nats"]
        if not servers:
            continue

        name = (doc.get("metadata") or {}).get("name", "?")
        checked += 1

        if not any(_reads_a_config_file(c) for c in servers):
            # No config file, so no accounts file, so no centrifugo user to leave open. The
            # guard is not required and its absence is not a finding.
            continue

        if not any(_is_the_guard(c) for c in (spec.get("initContainers") or [])):
            failures.append(
                f"StatefulSet/{name} starts nats-server with `-c` (so it reads "
                f"nats-accounts.conf and declares the `centrifugo` password user) but has no "
                f"initContainer that checks {PASSWORD_VAR}"
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

    docs = []
    for path in argv:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    failures, checked = evaluate(docs)

    # A gate that verified nothing must not report success.
    if checked == 0:
        print("ERROR: no NATS StatefulSet found; is this the right manifest?", file=sys.stderr)
        return 1

    if failures:
        print(
            f"FAIL: NATS would accept an unauthenticated Centrifugo connection "
            f"({len(failures)} of {checked} NATS StatefulSet(s)):\n",
            file=sys.stderr,
        )
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            f"\nAn empty {PASSWORD_VAR} parses cleanly and starts the server with a\n"
            "`centrifugo` user that accepts any client presenting no credential (finding 53).\n"
            "The guard lives in deploy/k8s/base/infra/nats.yaml; the local overlay deletes it\n"
            "by container name, so a rename there needs the matching rename in\n"
            "deploy/k8s/overlays/local/patches/nats-local.yaml.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: every NATS server reading an accounts file guards {PASSWORD_VAR} ({checked} checked)")
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
