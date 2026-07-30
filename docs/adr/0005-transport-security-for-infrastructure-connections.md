---
id: 0005
title: Authenticate and encrypt connections to NATS, Postgres and Redis, with a config surface rather than connection-string archaeology
status: Accepted
affects:
  - internal/config/**
  - internal/messaging/**
  - deploy/k8s/base/infra/**
  - deploy/k8s/base/services/**
  - deploy/k8s/overlays/**
  - cmd/natskeys/**
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

## Amendment 2026-07-29 — phase 2 implemented, six details settled

Phase 2 (NATS TLS) and finding 19 landed together as planned. The decision stands; these are
the details it left open, now settled by implementation and verified against a real k3s
cluster with cert-manager v1.21.0.

**Corrected premise.** This record says cert-manager's only issuer is a public Let's Encrypt
ClusterIssuer that cannot sign for `nats.hermes.svc`. On the cluster used for verification
there are **zero** ClusterIssuers — that issuer exists in `bootstrap-cluster.sh`, not
necessarily in any given cluster. The conclusion held for the right reason (a public ACME
issuer cannot sign an internal name) but it was reasoned, not observed.

1. **A namespaced `Issuer`, not a `ClusterIssuer`.** Everything needing an internal
   certificate lives in `hermes`. A namespaced issuer cannot be borrowed from another
   namespace, so if Centrifugo or the observability stack later need certificates from
   elsewhere this must change.
2. **A self-signed in-cluster CA.** Its private key sits in `hermes-internal-ca-tls` in the
   application namespace, so **anything that can read Secrets in `hermes` can mint a
   certificate every service trusts.** That is a real weakening and is accepted here only
   because the alternative — Vault or AWS Private CA — is a larger dependency than phase 2
   warrants. Revisit if the namespace gains untrusted workloads or broader Secret readers.
3. **`HERMES_NATS_CA_BUNDLE` is not required outside development.** Making `Validate` demand
   it was considered and rejected: without it, nats.go verifies against the system pool and
   **errors** rather than downgrading — verified, there is no silent-plaintext path — and
   requiring it would false-positive on an operator who baked the CA into the image or uses a
   publicly-trusted certificate. Fail-closed is carried by the `tls://` check plus
   `MustConnectNATS` exiting non-zero.
4. **Monitoring moved to `https` on 8222** with `scheme: HTTPS` probes. `/varz` exposes
   subjects, peers and connection details, and was previously readable in the clear.
5. **Server-only TLS; no `verify: true`.** Client certificates would authenticate a connection
   while still granting every client every subject, which is the argument this record already
   makes against mTLS-without-accounts. Authn/authz stays with phase 3's NKeys. Route TLS is
   mutual regardless, which is why the certificate carries client-auth usage.
6. **Server config in a hash-suffixed `secretGenerator`**, as this record specified. It holds no
   secret today; the benefit is that editing it rolls the cluster, and phase 3's NKeys land in
   the same file.

**Two operational consequences that are not obvious:**

- **`podManagementPolicy` is not updatable on a live StatefulSet.** Phase 2 changes it to
  `Parallel` (see finding 51), which on an existing NATS cluster requires delete-and-recreate,
  not a rolling update.
- **NATS does not reload certificates on file change**, only on SIGHUP. A renewal is therefore
  picked up at the next restart. Untested through an actual expiry cycle; a reloader annotation
  or SIGHUP sidecar may be wanted before certificates get short lifetimes.

## Amendment 2026-07-30 — phase 3 implemented; least privilege reached, with one named gap

Phase 3 (NKey accounts and per-service subject permissions) is done, and the decision stands
as written: one `HERMES` account, one NKey user per service, users scoped to the subjects they
use, credentials delivered through ExternalSecrets. What follows is what implementation
settled, verified against the same k3s cluster and cert-manager v1.21.0 phase 2 used, plus an
embedded `nats-server` in `make test`.

**The permissions were derived from an observed protocol trace, not from the subject list.**
Running `internal/messaging` against a traced JetStream server showed what the code actually
emits, and two of the four things it emits are not subjects at all:

- `SetupStreams` publishes `$JS.API.STREAM.UPDATE.<S>` *and* `$JS.API.STREAM.CREATE.<S>` —
  `CreateOrUpdateStream` tries UPDATE first and falls back on "stream not found", so both are
  required. Granting only CREATE makes first boot work and every boot after it hang.
- Consuming needs `$JS.API.CONSUMER.CREATE.<S>.<consumer>.<filter…>`,
  `$JS.API.CONSUMER.MSG.NEXT.<S>.<consumer>` and `$JS.ACK.<S>.<consumer>.>`. A missing
  JetStream grant produces no error — the request gets no reply and times out — which is why
  this record's warning about runtime failure understates it: it looks like a broken stream.

1. **A pull consumer needs no `subscribe` permission on the stream subject, and that moves the
   boundary.** Messages arrive on the client's own reply inbox, not on `delivery.email`. So
   `subscribe: _INBOX.>` — the obvious grant — would let any service receive copies of every
   other service's pulled messages, reading recipient addresses and rendered bodies with no
   `delivery.*` permission at all. `messaging.WithIdentity` therefore confines each connection
   to `_INBOX.<service>` and each user may subscribe to only its own prefix. **The client-side
   prefix and the server-side permission are one mechanism; changing either alone reopens the
   hole.** This was the least obvious finding of the phase and is verified in both directions.
2. **The permissions live in their own file, `nats-accounts.conf`, included by both server
   configurations.** Staging's `secretGenerator` changed from `behavior: replace` to `merge`:
   replace would have dropped the `accounts.conf` key, silently removing all authorisation from
   staging while leaving TLS in place. That is the exact failure this record warns about, and
   it would have been invisible in a rendered diff of `nats.conf`.
3. **Users are named by `$VARIABLE`, resolved from the server's environment.** Permissions stay
   reviewable in git; identities stay per-environment in the `nats-nkeys` Secret. Verified
   fail-closed in-cluster: with the Secret deleted the server refuses to start with
   `variable reference for 'HERMES_NKEY_SEND' ... can not be found` rather than starting with
   no accounts. `go run ./cmd/natskeys` generates a matched set.
4. **`HERMES_NATS_NKEY_SEED` is not required outside development**, following the same
   reasoning as `HERMES_NATS_CA_BUNDLE` in the phase 2 amendment: a server with accounts
   answers an unauthenticated CONNECT with an authorization violation, so an unset seed is a
   refused connection at startup, not a quieter one. Verified, both in `make test` and against
   the cluster.
5. **The inbox service lost its NATS connection entirely.** It held a `*messaging.Client` no
   code read. Under per-service credentials a connection costs an identity and a permission
   set, so the dead client was a credential granted for nothing; it is deleted rather than
   given a no-permission user.
6. **`nats:2-alpine` accepts the accounts configuration**, answering one of this record's "what
   I could not check" items. Verified on the image's current 2.14.3 and on the embedded 2.12.6.

**Where least privilege is not achieved, and why.** Every service calls `SetupStreams` at boot,
so **every user can create or update all four streams** — including streams it neither
publishes to nor consumes. Deletion, purge, direct get and listing are granted to nobody, so
the residual is stream-config tampering (an availability concern), not reading or forging
traffic. Fixing it properly means moving stream provisioning to a single identity, which trades
a startup ordering guarantee for the narrower grant; that was judged the worse deal while
`MustSetupStreams` exits non-zero on failure. Revisit if a provisioning job appears for other
reasons.

**Unresolved, and now urgent: Centrifugo cannot use this bus.** `centrifugo:v5` (5.4.9) exposes
exactly one NATS setting, `--nats_url`, documented as `nats://user:pass@host:4222`. There is no
NATS CA, TLS or NKey option. The base ConfigMap still sets `nats_url: nats://nats:4222` and
neither overlay changes it, so **Centrifugo's NATS broker was already incompatible with phase
2** — plaintext against a TLS-required server — and phase 3 adds authentication it also cannot
present. Phase 3 deliberately does **not** add a Centrifugo user: what credential form it can
actually present was not verified, and inventing one would be a guess. NATS accounts do support
password users alongside NKey users, so a password in `nats_url` is the likely path, but the TLS
half has no evident answer short of a sidecar or a plaintext listener. This blocks multi-node
Centrifugo fan-out in staging and production and is not a phase 3 regression.

## Status history

- 2026-07-29 — Accepted. Written before implementation, to unblock findings 1, 14 and 19,
  which the triage established are entangled and were repeatedly deferred as "large" without
  the shape of the work being decided.
- 2026-07-29 — Amended: phase 2 and finding 19 implemented and verified in-cluster; six open
  details settled above; the claim about the existing Let's Encrypt ClusterIssuer corrected
  from observed to reasoned. Phases 1 and 2 are done; **phase 3 (NKey accounts and
  per-service subject permissions) remains open** and is where authn/authz actually arrives.
- 2026-07-30 — Amended: phase 3 implemented and verified both in `make test` (embedded
  nats-server loading the committed permissions file) and against the k3s cluster. **All three
  phases are now done**, and finding 1 is closed. One residual over-grant is named above
  (stream create/update for every service), and one blocker is surfaced rather than solved
  (Centrifugo's NATS broker cannot present TLS or a credential).
