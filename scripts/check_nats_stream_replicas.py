#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Fail if the JetStream replication factor does not match the NATS cluster it runs on.

The same defect class as check_pdb_selectors.py: a durability control that renders, applies,
and does nothing.

JetStream defaults to one replica when `Replicas` is unset, and that is what the pipeline ran
with for its whole life -- every stream on a single peer, on a three-node StatefulSet, behind a
`minAvailable: 2` PDB and hard hostname anti-affinity that both exist to survive losing a node.
Nothing reported it. `nats stream ls` shows the streams as healthy, every publish succeeds, and
the first evidence is a node going away and taking NOTIFICATIONS or DELIVERY with it: publishes
fail, consumers stall, and any service that restarts crash-loops on EnsureStreams.

The value is deliberately explicit rather than derived at runtime -- a provisioner Job that
happened to run during a NATS rolling restart could observe one server and quietly downgrade
every stream. The cost of that choice is a config value that can disagree with reality, which
is exactly what this gate checks.

Two shapes are rejected.

  * **Streams less replicated than the cluster.** `HERMES_NATS_STREAM_REPLICAS` below the NATS
    StatefulSet's replica count. The silent single-peer case above.

  * **Streams more replicated than the cluster.** More replicas than there are servers, which
    JetStream refuses outright at provisioning time -- so the Job fails, and every service
    crash-loops on EnsureStreams behind it. Better caught in CI than in a deploy.

Equality is the only passing state, with one exception: a replication factor above 3 is allowed
on a larger cluster, because JetStream caps meaningful replication at 5 and three is already the
quorum this project deploys.

Usage:
    check_nats_stream_replicas.py rendered.yaml [...]
    kubectl kustomize deploy/k8s/overlays/production | check_nats_stream_replicas.py -
"""

import sys

REPLICAS_KEY = "HERMES_NATS_STREAM_REPLICAS"
PROVISION_JOB = "natsprovision"


def nats_server_count(docs):
    """Replicas of the NATS StatefulSet, or None when NATS is external to this render."""
    for doc in docs:
        if doc.get("kind") != "StatefulSet":
            continue
        name = (doc.get("metadata") or {}).get("name", "")
        if "nats" not in name:
            continue
        replicas = (doc.get("spec") or {}).get("replicas")
        return 1 if replicas is None else int(replicas)
    return None


def configured_replicas(docs):
    """The value HERMES_NATS_STREAM_REPLICAS resolves to, and where it came from.

    Checked in both places it can be set: a ConfigMap the provisioner reads via envFrom, and an
    explicit env entry on the Job itself, which wins.
    """
    from_configmap = None
    for doc in docs:
        if doc.get("kind") == "ConfigMap":
            value = (doc.get("data") or {}).get(REPLICAS_KEY)
            if value is not None:
                from_configmap = (int(value), "ConfigMap/" + (doc.get("metadata") or {}).get("name", "?"))

    for doc in docs:
        if doc.get("kind") != "Job":
            continue
        name = (doc.get("metadata") or {}).get("name", "")
        if PROVISION_JOB not in name:
            continue
        containers = (((doc.get("spec") or {}).get("template") or {}).get("spec") or {}).get("containers") or []
        for container in containers:
            for env in container.get("env") or []:
                if env.get("name") == REPLICAS_KEY and "value" in env:
                    return int(env["value"]), f"Job/{name} env"

    return from_configmap if from_configmap else (None, None)


def evaluate(docs):
    """Return (failure reason or None, servers, replicas, source)."""
    servers = nats_server_count(docs)
    replicas, source = configured_replicas(docs)

    if servers is None:
        return "no NATS StatefulSet in this render", servers, replicas, source
    if replicas is None:
        return (
            f"{REPLICAS_KEY} is not set anywhere in this render, so cmd/natsprovision falls back "
            f"to 1 and every stream lives on a single one of the {servers} NATS peers",
            servers, replicas, source,
        )

    if replicas < servers and not (replicas >= 3 and servers > 3):
        return (
            f"{REPLICAS_KEY}={replicas} against a {servers}-server NATS cluster: losing the one "
            f"peer holding a stream takes it offline, which is what the PDB and anti-affinity "
            f"rules in this overlay exist to prevent",
            servers, replicas, source,
        )
    if replicas > servers:
        return (
            f"{REPLICAS_KEY}={replicas} exceeds the {servers} available NATS server(s); "
            f"JetStream rejects the stream creation outright, so the provisioning Job fails and "
            f"every service crash-loops on EnsureStreams behind it",
            servers, replicas, source,
        )
    return None, servers, replicas, source


def report(failure, servers, replicas, source):
    """Print the verdict and return the exit code."""
    if servers is None:
        print(
            "ERROR: no NATS StatefulSet found; is this the right manifest? An overlay that "
            "points at an external NATS should not run this gate at all rather than have it "
            "pass vacuously.",
            file=sys.stderr,
        )
        return 1

    if failure:
        print(f"FAIL: {failure}", file=sys.stderr)
        print(
            "\nJetStream defaults to one replica when unset, and reports nothing unusual when it\n"
            "does: the streams list clean and every publish succeeds. The first evidence is a\n"
            "node going away and taking the stream with it. Set HERMES_NATS_STREAM_REPLICAS in\n"
            "the overlay's configMapGenerator to match the cluster size.",
            file=sys.stderr,
        )
        return 1

    print(f"ok: {REPLICAS_KEY}={replicas} matches the {servers}-server NATS cluster (from {source})")
    return 0


def main(paths):
    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; JetStream replica check not run", file=sys.stderr)
        return 0

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path)
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    return report(*evaluate(docs))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:] or ["-"]))
