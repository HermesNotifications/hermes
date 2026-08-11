#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if Centrifugo would refuse every browser websocket.

Another control that renders, applies, reports itself healthy, and serves nobody — the same
defect class as check_networkpolicy_selectors.py and check_pdb_selectors.py.

Centrifugo validates the `Origin` header against `allowed_origins` and answers **403 at the
websocket handshake** to anything unlisted. Crucially it *permits connections that carry no
Origin header at all*, "as they typically originate from non-browser environments". So with
`allowed_origins` unset:

  * `/health` is 200
  * `kubectl exec ... curl` connects
  * every server-side client connects
  * the pods are Ready and stay Ready
  * **no browser can connect, ever**

That combination is why this went unnoticed. The local overlay shipped without the key and the
first live browser run failed 24 of 42 specs, each waiting 30s for a realtime status that a 403
had already made impossible — 45 minutes to be told nothing more than "connecting".

It matters more in staging and production than locally, because the inbox widget is *embedded
in a customer's application*. The browser presents their origin, never the Hermes domain, so
cross-origin is the normal case rather than the exception.

## What is rejected

  * **No Centrifugo workload at all** — the gate would otherwise pass by verifying nothing.
  * **The key absent**, in both the env var and the ConfigMap. The actual bug.
  * **An empty value**, which Centrifugo reads as same-origin only.

## What is accepted

The documented deploy-time placeholder, by default. The overlays commit
`ALLOWED_ORIGINS_PLACEHOLDER` exactly as the production ingress commits `DOMAIN_PLACEHOLDER`,
and substitution happens in the deploy pipeline — so failing on it here would fail `make
verify` on the very files the repository is meant to contain. Pass `--forbid-placeholder` to
reject it, which is what a deploy pipeline should do *after* substituting.

## Precedence

`CENTRIFUGO_ALLOWED_ORIGINS` on the Deployment wins over `allowed_origins` in the mounted
config, because Centrifugo's env vars override the config file. The gate resolves them in that
order so it reports on the value that will actually be in force.

Usage:
    check_centrifugo_origins.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/staging | check_centrifugo_origins.py -
    kubectl kustomize deploy/k8s/overlays/production | check_centrifugo_origins.py - --forbid-placeholder
"""

import json
import sys

ENV_VAR = "CENTRIFUGO_ALLOWED_ORIGINS"
PLACEHOLDER = "ALLOWED_ORIGINS_PLACEHOLDER"
CONFIG_KEY = "allowed_origins"


def centrifugo_containers(docs):
    """(workload name, container) for every container running Centrifugo.

    Matched on image rather than on name, so a renamed Deployment or a sidecar layout does not
    quietly drop out of the gate's view.
    """
    out = []
    for doc in docs:
        if doc.get("kind") not in ("Deployment", "StatefulSet"):
            continue
        name = (doc.get("metadata") or {}).get("name", "?")
        spec = (((doc.get("spec") or {}).get("template") or {}).get("spec")) or {}
        for container in spec.get("containers") or []:
            if "centrifugo" in str(container.get("image", "")):
                out.append((name, container))
    return out


def env_value(container, key):
    """The literal value of `key`, or None when absent or sourced from a Secret/ConfigMap.

    A valueFrom reference is treated as absent rather than as satisfying the check: the gate
    cannot see what it resolves to, and claiming otherwise would be the same false assurance
    it exists to prevent.
    """
    for entry in container.get("env") or []:
        if entry.get("name") == key:
            return entry.get("value")
    return None


def config_origins(docs):
    """`allowed_origins` from a centrifugo-config ConfigMap, or None if there is none.

    Reads config.json under any key, since the generator suffixes ConfigMap names with a hash
    and the file may be mounted under a different key in a future layout.
    """
    for doc in docs:
        if doc.get("kind") != "ConfigMap":
            continue
        name = (doc.get("metadata") or {}).get("name", "")
        if "centrifugo" not in name:
            continue
        for value in (doc.get("data") or {}).values():
            try:
                parsed = json.loads(value)
            except (TypeError, ValueError):
                continue
            if CONFIG_KEY in parsed:
                return parsed[CONFIG_KEY]
            # v6 nests it under `client`; accepted here so the gate keeps working if the
            # overlays move off the v5 image, rather than silently passing on a missing key.
            client = parsed.get("client")
            if isinstance(client, dict) and CONFIG_KEY in client:
                return client[CONFIG_KEY]
    return None


def normalise(value):
    """Centrifugo accepts a space-separated string (env) or a list (config file)."""
    if value is None:
        return None
    if isinstance(value, str):
        return [item for item in value.split() if item]
    if isinstance(value, list):
        return [str(item) for item in value if str(item)]
    return []


def evaluate(docs, forbid_placeholder=False):
    """Return (failures, workloads checked).

    `failures` is a list of (workload name, reason). Split out from main() so the decision is
    testable without rendering YAML, as the other gates here do it.
    """
    failures = []
    containers = centrifugo_containers(docs)
    from_config = normalise(config_origins(docs))

    for name, container in containers:
        origins = normalise(env_value(container, ENV_VAR))
        source = f"{ENV_VAR} env var"
        if origins is None:
            origins, source = from_config, "allowed_origins in the mounted config"

        if origins is None:
            failures.append((
                name,
                f"no {ENV_VAR} env var and no {CONFIG_KEY} in its config; Centrifugo will "
                "answer 403 to every browser websocket while every non-browser client, "
                "including the health probe, keeps succeeding",
            ))
            continue

        if not origins:
            failures.append((
                name,
                f"{source} is empty, which means same-origin only. An embedded widget is "
                "cross-origin by construction, so this refuses every customer page",
            ))
            continue

        if forbid_placeholder and PLACEHOLDER in origins:
            failures.append((
                name,
                f"{source} still carries {PLACEHOLDER}; substitute the real origins before "
                "deploying",
            ))

    return failures, len(containers)


def report(failures, checked):
    """Print the verdict and return the exit code. Separated so the sentinel is testable."""
    # Verifying nothing is the failure mode this gate exists to prevent, so an absent
    # Centrifugo is an error rather than a quiet pass.
    if checked == 0:
        print(
            "ERROR: no Centrifugo workload found; is this the right manifest? If realtime was "
            "deliberately removed from this overlay, drop the gate's step rather than letting "
            "it pass by checking nothing.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} Centrifugo workloads refuse browser "
              f"websockets:\n", file=sys.stderr)
        for name, reason in failures:
            print(f"  {name}: {reason}", file=sys.stderr)
        print(
            "\nCentrifugo validates Origin against allowed_origins and 403s the handshake for\n"
            "anything unlisted — but permits connections with no Origin header at all. So the\n"
            "service stays Ready, /health stays 200, curl keeps working, and only browsers are\n"
            "turned away. Set CENTRIFUGO_ALLOWED_ORIGINS on the Centrifugo Deployment.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} Centrifugo workloads declare allowed_origins")
    return 0


def main(argv):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; Centrifugo origin check not run", file=sys.stderr)
        return 0

    forbid_placeholder = "--forbid-placeholder" in argv
    paths = [arg for arg in argv if not arg.startswith("--")]
    if not paths:
        print(__doc__, file=sys.stderr)
        return 2

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    return report(*evaluate(docs, forbid_placeholder))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
