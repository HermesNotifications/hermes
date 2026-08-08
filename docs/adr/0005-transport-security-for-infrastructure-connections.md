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

**Status:** Accepted (2026-07-29; amended 2026-07-31: the Centrifugo password's first character
must be an ASCII letter — a cross-component contract binding `cmd/natskeys`, `nats-accounts.conf`
and anyone setting the Secret by hand; amended 2026-08-05: review findings 38/39 referenced
below renumbered to 52/53, references only)  
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

## Amendment 2026-07-30 — phase 4: the three things phases 1–3 left open

Phase 3 closed finding 1 and named three residual items. All three are now settled. The
decision stands; two of the three change details this record previously argued the other way,
and both departures are set out below with what changed the answer. Verified against the same
k3s cluster and cert-manager v1.21.0, plus the embedded `nats-server` in `make test`.

### 1. Centrifugo can use the bus after all — TLS *and* a credential

The phase 3 amendment recorded that `centrifugo:v5` "exposes exactly one NATS setting,
`--nats_url`" and that "the TLS half has no evident answer short of a sidecar or a plaintext
listener". **That was wrong, and wrong in a way worth recording: `--help` is not the
configuration surface.** Centrifugo registers configuration keys that have no flag equivalent,
and one of them is `nats_tls`. Established by reading the image's registered keys and struct
tags and then executing every candidate against a TLS-required `nats-server`:

```json
"nats_url": "tls://nats:4222",
"nats_tls": { "enabled": true, "server_ca_pem_file": "/etc/nats-certs/ca.crt" }
```

Three details are load-bearing and each was observed, not inferred:

- **`nats_tls` is a map, not a bool.** `"nats_tls": true` is rejected with
  `error configuring nats tls: extract TLS config: '' expected a map, got 'bool'`. The
  overlays' existing `CENTRIFUGO_REDIS_TLS: "true"` *is* a bool, so the natural guess by
  analogy is the wrong one — which is most likely why phase 3 concluded the option did not
  exist.
- **`"enabled": true` is required.** With it false the CA is ignored and the connection fails
  `x509: certificate signed by unknown authority`.
- **A plaintext `nats://` URL was never the actual failure.** nats.go upgrades to TLS when the
  server advertises `tls_required`, so the pre-phase-4 base ConfigMap failed on *CA
  verification*, not on a refused plaintext connection. The practical difference matters: there
  was never a silent-plaintext path here either.

**Centrifugo is a password user, not an NKey user.** No NKey setting exists in any form — no
flag, no config key, no environment variable. The credential can only travel in `nats_url`'s
userinfo, so `nats-accounts.conf` gains a `centrifugo` password user beside the six NKey users,
which NATS supports natively. **A client certificate would have been possible**
(`nats_tls.cert_pem_file`/`key_pem_file` exist) and was not used: this record already argues
that mTLS without accounts authenticates a connection while granting every subject, and a
password user with a subject scope is the thing that actually constrains Centrifugo.

Its subject space is granted as the prefix `centrifugo.>` in both directions rather than as the
three observed patterns. That restricts nothing further — all three of `centrifugo.control`,
`centrifugo.node.<nodeID>` and `centrifugo.client.<channel>` are needed for publish *and*
subscribe — while avoiding a trap: a subject added by a future Centrifugo version would fail
**silently**. Observed with the subscribe grant narrowed: the server logs
`Permissions Violation for Subscription`, the client stays connected, and publications simply
never arrive. Nothing surfaces to the subscriber.

Two consequences that are not obvious:

- **The password is stored twice** — as `centrifugo_password` for nats-server and inside
  `centrifugo_nats_url` for Centrifugo — because Centrifugo has no separate password setting.
  Nothing in the cluster checks that they agree. `cmd/natskeys` therefore emits both from one
  generated value, and a test asserts the URL round-trips the password through `url.Parse`.
- **nats-server holds a plaintext password in its process environment.** NATS supports bcrypt
  for password users, which would keep only a hash server-side; it is not used here because the
  two halves would then need generating and rotating as a pair with no cross-check. Worth
  revisiting if the NATS config Secret and the client Secret ever diverge in blast radius.

### 2. Stream provisioning is one identity — reversing this record's earlier judgement

Phase 3 judged a provisioning identity "the worse deal while `MustSetupStreams` exits
non-zero", because self-declaration means any service can heal a missing stream at boot. **That
trade was framed as a binary and it is not one.** The startup-ordering guarantee comes from
services *failing closed on a stream they need*, not from their being able to create it. So
services now **verify** instead of declaring:

- `cmd/natsprovision` runs as a Job and is the only identity holding
  `$JS.API.STREAM.CREATE.<S>` and `…UPDATE.<S>`.
- Services hold `$JS.API.STREAM.INFO.<S>` — a read — for only the streams in
  `messaging.StreamsForService`, and `MustEnsureStreams` exits non-zero if one is absent.

The guarantee phase 3 wanted to keep is kept; what is given up is self-healing. That is the
same bargain the repository already makes for the database: streams are schema, and schema is
provisioned by a Job that runs first, with a crash-loop as the convergence mechanism. Verified
by running the real binaries against a real TLS+NKey server: a worker on a fresh bus exits 1
with `stream DELIVERY is not available to hermes-worker-email (has cmd/natsprovision run?)`,
and starts normally once the Job's binary has run.

`STREAM.INFO` is a new grant and does leak stream configuration and message counts to the
services that depend on that stream. That is a read of metadata, not of traffic, and it is
scoped per stream — `TestProvisioning_ServicesCannotReadStreamsTheyDoNotDependOn` asserts the
refusals. **`STREAM.DELETE`, `STREAM.PURGE`, `CONSUMER.DELETE`, `DIRECT.GET`, `STREAM.MSG.GET`,
`STREAM.LIST` and `$JS.API.INFO` remain granted to nobody, the provisioner included** — which
is why removing streams during verification required a fresh server rather than a delete call.

The cost is a new deployable: an image, a CI matrix entry, a Kargo subscription, a Job, and one
more NKey. The Job inherits `hermes-migrate`'s unsolved problem — a Job's pod template is
immutable, so re-applying a changed one needs an Argo PreSync hook, which is commented out for
both.

### 3. The CA private key is out of the application namespace — departing from amendment 1

The phase 2 amendment chose a namespaced `Issuer` because "a namespaced issuer cannot be
borrowed by a workload in another namespace", and accepted that the CA key therefore sat in
`hermes`. The CA is now a `ClusterIssuer`, with the signing Secret in cert-manager's
cluster-resource namespace (`--cluster-resource-namespace=$(POD_NAMESPACE)` = `cert-manager`,
read off the running Deployment). Verified: a Certificate in an unrelated namespace issued from
a `ca` ClusterIssuer whose Secret exists only in `cert-manager`, and only the leaf — `ca.crt`,
`tls.crt`, `tls.key`, `CA:FALSE` — landed in the requesting namespace.

**Phase 2's reasoning was not wrong, and its cost is real.** A ClusterIssuer can be used by a
Certificate in *any* namespace; that was verified too, by minting a certificate with SAN
`nats.hermes.svc` from a namespace that is not `hermes`. This is a trade, made deliberately:

- The CA key is the ten-year root of the entire internal trust domain and is never rotated.
  Reading it is silent, offline, untraceable, and yields unlimited certificates for any
  identity — including identities that do not exist yet.
- Requesting a leaf is a logged Kubernetes API write that leaves `Certificate` and
  `CertificateRequest` objects behind, needs create permission on cert-manager CRDs, and yields
  one 90-day certificate.
- Secret-read in an application namespace is a common grant that tends to widen — this record's
  own phase 2 amendment says "revisit if the namespace gains broader Secret readers".
  Cluster-wide Certificate-create is rarer and is constrainable by admission policy or by
  cert-manager's CertificateRequest approval RBAC.

Trading an unbounded invisible compromise for a bounded auditable one is worth it. **The
residual is not closed:** any namespace may still request a certificate this CA signs, and
nothing in this repository constrains that. Closing it needs admission policy or approver RBAC,
neither of which is in scope here.

Two things this depends on that are easy to break:

- **`deploy/k8s/pki` must not be reached from inside `base/`.** `base/kustomization.yaml` sets
  `namespace: hermes`, and the kustomize namespace transformer would rewrite the CA
  Certificate's `namespace: cert-manager` — putting the key straight back where this change
  removes it from, with a manifest that applies cleanly and certificates that still issue.
  Nothing about the failure is visible in behaviour, so
  `scripts/check_ca_key_location.py` fails the build on it and runs in `make verify`.
- **The hardcoded `cert-manager` namespace couples Hermes to where cert-manager runs.** If they
  disagree the ClusterIssuer reports `Ready: False` with
  `ErrGetKeyPair: ... secrets "hermes-internal-ca-tls" not found` and issues nothing — loud,
  observed, but it is a coupling that did not exist before. Hermes' manifests now also write one
  resource into a namespace they do not own.

### What this phase could not check

- **Nothing was applied to a real staging or production cluster.** No AWS, no ESO: the new
  `HERMES_CENTRIFUGO_NATS_URL`, `HERMES_CENTRIFUGO_NATS_PASSWORD` and provisioner ExternalSecret
  entries are structurally valid and untested against a real secret store.
- **`hermes-natsprovision` has never been run as a Kubernetes Job.** The binary was verified
  against a real TLS+NKey server in a container with the same configuration the Job supplies;
  the Job manifest itself, its ordering against services, and its image tag reaching staging
  through Kargo are unexercised.
- **Two Hermes installs in one cluster would now collide** on the cluster-scoped ClusterIssuer
  names and the CA Secret name. They already could not coexist — `base/kustomization.yaml` pins
  both overlays to the `hermes` namespace — so this adds no new constraint today, but it removes
  the option.
- **Certificate renewal through the new ClusterIssuer was not exercised through an expiry
  cycle**, and neither was the phase 2 note that NATS reloads certificates only on SIGHUP.
- **Centrifugo's Redis auth in production** (finding 41's second half) was not touched. The
  production ExternalSecret does now carry `HERMES_CENTRIFUGO_REDIS_PASSWORD`, but its
  `centrifugo-env.yaml` patch still does not set `CENTRIFUGO_REDIS_PASSWORD` or the Redis TLS
  variables that staging sets — a separate, still-open gap.

## Amendment 2026-07-31 — the Centrifugo password's first character is a cross-component contract

Phase 4 introduced `centrifugo` as a password user (amendment item 1 above) and left the
password's *shape* unstated, on the reasonable assumption that a password is an opaque string.
It is not, and the gap is recorded here because it binds three components that have no other
contact with each other.

**The constraint.** The generated Centrifugo NATS password **must begin with an ASCII letter**.

**Why.** `nats-accounts.conf` carries the credential as an unquoted variable reference,
`password: $HERMES_CENTRIFUGO_NATS_PASSWORD`. nats-server does not substitute such a reference:
`conf/parse.go`'s `lookupVariable` resolves it by **re-parsing the environment value as a fresh
configuration document**. The password therefore has to lex as a bare conf value. Enumerated
against the real parser (nats-server v2.12.6) rather than modelled, the failing shapes are a
leading `-`, a leading digit whose first non-digit is `-`, and a leading digit followed by a
size suffix and another digit. A leading ASCII letter is always safe whatever follows.
Unconstrained 43-character base64url hits a failing shape in **2.33%** of draws (465 of 20,000
through the real parser), which is why it presented as an intermittent, differently-worded test
failure rather than as a configuration bug.

**Why this is an ADR amendment and not just a code comment.** The constraint is currently
enforced in `cmd/natskeys` and described in `docs/configuration.md`, and that is the whole of
it — but it binds:

- **the generator** (`cmd/natskeys`), which must redraw until the first character is a letter;
- **the config file** (`deploy/k8s/base/infra/nats-accounts.conf`), whose reference must stay
  **unquoted** — quoting is not a fix and must never be applied. NATS reaches `isVariable()`
  only from `lexString`, so both `"$HERMES_…"` and `'$…'` resolve Centrifugo's password to the
  *literal* variable name: no parse error, no log line, a credential published in git, and
  Centrifugo unable to authenticate. Strictly worse than the bug;
- **any human** who sets `HERMES_CENTRIFUGO_NATS_PASSWORD` by hand with
  `kubectl create secret` or by editing Secrets Manager, who has no other way to learn this.

A rule that three independent parties must honour, where violating it stops the whole bus
starting, is a cross-component contract — and one that is invisible in each component
separately, which is exactly the kind this record exists to hold.

**Scope.** The `$HERMES_NKEY_*` references have identical exposure and are safe **by luck, not
by design**: an nkeys user public key is `U` followed by base32 `[A-Z2-7]`, so it always begins
with a letter and never contains `-`. That is now asserted
(`TestAccounts_NKeyVariablesCannotHitTheFailingShapes`) rather than assumed, so a key-format
change becomes a red test rather than a cluster that will not start.

**Still open, and not closed by this amendment (finding 53, issue #82).** An *empty*
`HERMES_CENTRIFUGO_NATS_PASSWORD` parses cleanly, starts the server, and lets a client connect
as `centrifugo` with no credential at all — verified on the wire. The conf language cannot
express "must be non-empty", so it cannot be closed in the file that documents the guarantee.
The owner is now determined: an initContainer on the NATS StatefulSet
(`deploy/k8s/base/infra/nats.yaml`), which is the only candidate that can deliver "a
half-provisioned cluster fails to start" rather than reporting the problem after nats-server is
already serving. `internal/config` cannot (no Hermes process reads the variable) and
`cmd/natsprovision` should not (it would have to impersonate `centrifugo` with an empty
credential on every deploy). The complication a fix must handle is that the local overlay drops
`-c nats.conf` and legitimately has no password, so the guard needs a matching removal patch
there. The behaviour is pinned meanwhile by
`TestCentrifugoPassword_EmptyVariableIsAcceptedAndAuthenticates`, which is deliberately **not**
flipped: it characterises nats-server's parser, which is upstream and unchanged.

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
- 2026-07-30 — Amended: **phase 4**, closing all three items phase 3 left open. Centrifugo now
  reaches the bus over TLS as a password user — the phase 3 amendment's claim that
  `centrifugo:v5` has no TLS option is **corrected**, it has an undocumented `nats_tls`
  configuration key with no flag equivalent. Stream declaration moved to a single provisioning
  identity (`cmd/natsprovision`), **reversing** phase 3's judgement that the trade was not worth
  making, because services can fail closed on a stream without being able to create it. The CA
  private key moved out of the `hermes` namespace into a `ClusterIssuer`, **departing** from the
  phase 2 amendment's preference for a namespaced `Issuer`; that departure is a deliberate trade
  and its residual — any namespace may request a certificate this CA signs — is named and not
  closed.
- 2026-07-31 — Amended (clarification, no decision changed): the Centrifugo password's **first
  character must be an ASCII letter**, because `nats-accounts.conf` references it unquoted and
  nats-server resolves such a reference by re-parsing the value as a configuration document.
  ~2.3% of unconstrained base64url draws otherwise stop the server starting. Recorded here
  rather than left in `cmd/natskeys` alone because it binds the generator, the config file (whose
  reference must stay **unquoted** — quoting silently publishes the literal variable name as the
  password) and any human creating the Secret by hand. Finding 53 — an *empty* password
  authenticates — remains open, but its owner is now determined: an initContainer on the NATS
  StatefulSet, not `internal/config` and not `cmd/natsprovision`, with reasons recorded above.
- 2026-08-05 — Amended (correction, no decision changed): the two review findings referenced
  above were filed as 38 and 39, numbers already held by other findings, and are now **52**
  (the unquoted `$VARIABLE`) and **53** (the empty password). Only the references changed.
