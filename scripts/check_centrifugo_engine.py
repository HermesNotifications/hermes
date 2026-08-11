#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if Centrifugo runs more than one replica on the in-memory engine.

The sibling of check_centrifugo_origins.py, and the same shape of defect: a realtime deployment
that looks entirely healthy and delivers nothing.

With the memory engine each Centrifugo node keeps its own subscription registry. Hermes
publishes over the HTTP API to whichever node the Service happens to route it to, and that node
delivers only to the clients connected to *itself*. At one replica that is correct. At two it
silently loses roughly half of every user's notifications -- no error, no log line, no failed
health check, and `centrifugo_node_num_clients` looks fine on both pods.

docs/self-hosting/production.md has described this hazard in prose since the chart shipped.
Prose does not fail a render.

The rule is deliberately narrow: memory engine with replicas > 1. A single-replica evaluation
install is a legitimate posture and stays passing; anything with a shared engine (redis) passes
at any replica count.

Usage:
    check_centrifugo_engine.py rendered.yaml [...]
    helm template r charts/hermes | check_centrifugo_engine.py -
"""

import json
import sys

WORKLOAD_KINDS = ("Deployment", "StatefulSet")


def centrifugo_configs(docs):
    """(configmap name, parsed config) for every rendered Centrifugo config document."""
    out = []
    for doc in docs:
        if doc.get("kind") != "ConfigMap":
            continue
        name = (doc.get("metadata") or {}).get("name", "?")
        if "centrifugo" not in name:
            continue
        for filename, body in (doc.get("data") or {}).items():
            if not filename.endswith(".json"):
                continue
            try:
                out.append((name, json.loads(body)))
            except json.JSONDecodeError as exc:
                out.append((name, {"__parse_error__": str(exc)}))
    return out


def engine_type(config):
    """The configured engine, across both schema generations.

    v5 is flat (`"engine": "redis"`); v6 nests it (`engine: {type: redis}`). Centrifugo defaults
    to memory when the key is absent, and so do we -- assuming the safe value here would let an
    omission pass.
    """
    engine = config.get("engine")
    if isinstance(engine, dict):
        return engine.get("type", "memory")
    if isinstance(engine, str):
        return engine
    return "memory"


def centrifugo_replicas(docs):
    """Replica count of the Centrifugo workload, or None if it renders none."""
    for doc in docs:
        if doc.get("kind") not in WORKLOAD_KINDS:
            continue
        name = (doc.get("metadata") or {}).get("name", "")
        if "centrifugo" not in name:
            continue
        replicas = (doc.get("spec") or {}).get("replicas")
        return 1 if replicas is None else int(replicas)
    return None


def evaluate(docs):
    """Return (failures, configs checked)."""
    failures = []
    configs = centrifugo_configs(docs)
    replicas = centrifugo_replicas(docs)

    for name, config in configs:
        if "__parse_error__" in config:
            failures.append((name, f"config is not valid JSON: {config['__parse_error__']}"))
            continue
        engine = engine_type(config)
        if engine == "memory" and replicas is not None and replicas > 1:
            failures.append((
                name,
                f"the in-memory engine is configured but Centrifugo runs {replicas} replicas. "
                "Each node keeps its own subscription registry, so a publication reaches only "
                "the clients connected to the node that received it -- silently losing the "
                "rest, with nothing in any log or health check to say so",
            ))
    return failures, len(configs)


def report(failures, checked):
    if checked == 0:
        print(
            "ERROR: no Centrifugo config found; is this the right manifest? An install with no "
            "Centrifugo at all should not run this gate rather than have it pass vacuously.",
            file=sys.stderr,
        )
        return 1

    if failures:
        print(f"FAIL: {len(failures)} of {checked} Centrifugo configs cannot deliver reliably:\n",
              file=sys.stderr)
        for name, reason in failures:
            print(f"  ConfigMap/{name}: {reason}", file=sys.stderr)
        print(
            "\nEither drop to a single replica (the evaluation posture) or configure the Redis\n"
            "engine and a broker. See docs/self-hosting/production.md.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: all {checked} Centrifugo configs can fan out across their replica count")
    return 0


def main(paths):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; Centrifugo engine check not run", file=sys.stderr)
        return 0

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    return report(*evaluate(docs))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:] or ["-"]))
