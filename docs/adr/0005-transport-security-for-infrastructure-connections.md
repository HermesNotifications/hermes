---
id: 0005
title: Authenticate and encrypt connections to NATS, Postgres and Redis, with a config surface rather than connection-string archaeology
status: Accepted
affects:
  - internal/config/**
  - internal/messaging/**
  - deploy/k8s/base/infra/nats.yaml
  - infra/crossplane/**
source: docs/reviews/2026-07-27-architecture-review.md — findings 1, 14 and 19; triage of 2026-07-29
---

# ADR 0005: Transport security for infrastructure connections

**Status:** Accepted (2026-07-29)  
**Date:** 2026-07-29  
**Author:** Daryl Robbins

---

## Context

Findings 1 and 14 of the 2026-07-27 architecture review describe one problem from two angles.

**NATS has no authentication and no encryption.** `internal/messaging/nats.go` connects with a
bare `nats.Connect(url)` — no token, NKey, credentials or TLS option, and no config field
exists to supply one. The deployment starts the server with no auth either:
`deploy/k8s/base/infra/nats.yaml` passes `--jetstream`, `--store_dir`, `--cluster_name`,
`--cluster` and `-m 8222`, mounts only a data PVC, and reads no configuration file. Anyone
with network reach can publish to `notification.send` to forge notifications, or subscribe to
`delivery.*` to read every recipient address and rendered body in flight. This is the trust
boundary of the entire pipeline, and it is currently open.

**No datastore has a TLS configuration surface.** A repo-wide search across
`internal/store/`, `internal/cache/`, `internal/config/` and `internal/messaging/` for
`tls`/`sslmode`/`RootCAs` returns exactly one hit: the `sslmode=disable` in the default
database URL. Postgres goes through `pgxpool.ParseConfig` and Redis through
`redis.ParseURL`, so TLS is reachable — but only by hand-authoring a connection string, and
with no way at all to supply a CA bundle, a client certificate, or a verification mode.

The practical consequence is worse than the missing capability: **the repository cannot
answer whether TLS is on in production.** The URLs come from AWS Secrets Manager, nothing in
the repo assembles them, and the triage had to record finding 14's production posture as
CANNOT-VERIFY-FROM-REPO. A security property nobody can determine from the source is not a
security property.

Two facts change the shape of the work, and both were checked rather than assumed:

- **The Redis half is nearly free.** `infra/crossplane/compositions/aws/cache.yaml` already
  sets `atRestEncryptionEnabled: true`, wires an `authTokenSecretRef` with
  `authTokenUpdateStrategy: ROTATE`, and maps `spec.transitEncryption` through to
  `transitEncryptionEnabled`. Production already claims `transitEncryption: true`. The
  infrastructure is provisioned for authenticated TLS; only the client is not using it.
- **cert-manager is installed but cannot currently issue the certificates we need.**
  `infra/scripts/bootstrap-cluster.sh` installs cert-manager and creates a Let's Encrypt
  ClusterIssuer. That is a public ACME issuer: it can issue for the ingress domain and cannot
  issue for `nats.hermes.svc`. Internal certificates need a second, private issuer. The tool
  is present; the configuration for this use is not.

## Decision

Secure infrastructure connections in two layers, and give both an explicit configuration
surface rather than encoding them in connection strings.

**1. Datastores.** Add per-datastore TLS configuration to `internal/config` — verification
mode and CA bundle path, alongside the existing URL. Outside development, TLS is **required
and fails closed**: a service configured to reach a real datastore without TLS refuses to
start rather than connecting in the clear.

**2. NATS.** Authenticate clients with NKey credentials and encrypt with TLS, using a server
configuration file mounted from a Secret. Each service gets its own user, and users are
scoped to the subjects they actually use. Credentials are delivered through ExternalSecrets,
as other secrets already are.

**3. Certificates.** Add a private cert-manager issuer for in-cluster certificates. The
existing Let's Encrypt ClusterIssuer stays for ingress and is not reused for internal traffic.

Ship it in three phases, in this order:

| Phase | Scope | Blocked by |
|---|---|---|
| 1 | Config surface, fail-closed defaults, Redis `rediss://` + auth token | Finding 12 for Postgres — see Consequences |
| 2 | NATS TLS: private issuer, server certificates, client trust | Phase 1's config surface |
| 3 | NATS accounts and per-service subject permissions | Phase 2 |

The phases are separately shippable and separately valuable. Phase 1 alone closes finding 14
and most of the exposure, because it turns the datastore connections — which carry the same
recipient data as the bus — from unverifiable to enforced.

## Why

**Why a config surface rather than connection strings.** The alternative is to keep TLS
URL-driven and simply document that operators should write `sslmode=verify-full`. It was
rejected because it is the status quo, and the status quo produced a security property that
could not be verified from the repository. A config field can have a default that fails
closed; a substring of a secret retrieved at runtime cannot. This is the decision's real
content: not "use TLS" — nobody disagrees with that — but "make the setting legible and
enforceable in code".

**Why NKeys rather than a shared token.** Token auth is one line of server config and was
seriously considered. It was rejected because a single bearer token in every pod gives no
per-service identity, so subject-level authorization is impossible and rotation means
restarting every service simultaneously. NKeys give each service its own identity, which is
what makes phase 3 possible at all. The cost is real: credential distribution becomes nine
secrets rather than one.

**Why not a service mesh.** Istio or Linkerd would provide mTLS for every connection here
without application changes, and would arguably solve findings 1 and 14 together. Rejected:
it introduces a large operational component — control plane, sidecar injection, upgrade
cadence — to solve one problem in a system that has no other need for it, and nothing in this
repository or its history suggests the mesh expertise to operate one. Revisit if a second
independent driver for a mesh appears.

**Why not mTLS-only for NATS, without accounts.** Client certificates alone authenticate the
connection but grant every authenticated client full access to every subject. The isolation
the review asks for is per-service subject permissions, and in NATS that means accounts and
users. mTLS without accounts would look like a fix and would not restrict what a compromised
worker can publish.

**Why fail closed.** A default that silently degrades to plaintext is how the current defect
arose — `sslmode=disable` sits in a default nobody reads, and `mail.NoTLS` did the same thing
in `internal/email` (finding 13, fixed 2026-07-29). Both were silent. The cost of failing
closed is that a misconfigured service refuses to start, which is loud and recoverable; the
cost of failing open is data on the wire, which is silent and is not.

## Consequences

- **Phase 1's Postgres half depends on finding 12.** `HERMES_DATABASE_URL` is assembled from
  the Crossplane connection details — which expose `password`, `endpoint`, `port` and
  `username` separately — and the `HermesSecretsBundle` composition that should assemble them
  is a no-op. Whoever fixes finding 12 chooses the `sslmode`. These two findings must be
  sequenced together or Postgres TLS cannot be turned on at all.
- **Finding 19 touches the same StatefulSet.** Adding a `securityContext` to NATS and mounting
  a config file both change `deploy/k8s/base/infra/nats.yaml` and both force a rolling restart
  of the NATS cluster. Do them in one change, not two.
- **Phase 3 changes a published contract in spirit.** Per-service subject permissions mean
  that adding a subject to a service requires updating that service's user permissions. A
  service that gains a publish target and does not gain the permission fails at runtime, not
  at deploy. **What detects it:** the DLQ. A permissions error surfaces as a publish failure,
  which `internal/messaging` classifies and dead-letters — but only once finding 9's delivery
  path actually reaches that machinery.
- **Local development must keep working without certificates.** `make infra-up` runs NATS with
  no TLS and no auth, and that should stay true. The fail-closed default therefore keys on
  environment, and the dev path must be an explicit opt-out rather than the absence of
  configuration — otherwise the production default is one unset variable away from plaintext,
  which is the defect this record exists to remove.
- **Nothing is deployed yet**, so all three phases are cheap now. Enabling TLS on a live NATS
  cluster is a coordinated restart; enabling it before the cluster exists is a config change.

## What I could not check

- **Whether Aurora is configured to require TLS**, as opposed to merely supporting it. RDS
  supports `rds.force_ssl`, and nothing in `infra/crossplane/compositions/aws/database.yaml`
  sets it. Verifying needs a deployed cluster.
- **Whether the RDS CA bundle is available in-cluster.** `verify-full` needs the Amazon RDS
  root certificate mounted somewhere. No mechanism for that exists in the repo today, and the
  effort in phase 1 depends on whether it can come from the container image or must be a
  ConfigMap.
- **Whether `nats:2-alpine` accepts the accounts configuration** phase 3 needs without an
  image change. The NATS server supports it; that this image and version do was not executed.
- **Whether ElastiCache's auth token actually reaches Centrifugo in production.** Finding 41
  found the production `centrifugo-env.yaml` patch stops at the address and never sets
  `CENTRIFUGO_REDIS_PASSWORD`, while staging does. Phase 1 will surface this immediately, and
  it should be fixed as part of it rather than discovered during it.
- **The performance cost of TLS on the JetStream hot path.** Unmeasured. `cmd/dispatchbench`
  exists and would measure it; that has not been run.

## Status history

- 2026-07-29 — Accepted. Written before implementation, to unblock findings 1, 14 and 19,
  which the triage established are entangled and were repeatedly deferred as "large" without
  the shape of the work being decided.
