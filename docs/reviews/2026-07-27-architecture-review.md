# Architecture Review — 2026-07-27

Findings from a full review of the architecture documentation, the code it describes, and
the deployment/infrastructure configuration.

**Reviewed at:** `68f0996` (main)  
**Last updated:** 2026-07-29, after a full triage sweep re-verified every open finding
against the tree — see [Triage 2026-07-29](#triage-2026-07-29--all-open-findings-re-verified)  
**Scope:** `docs/` (all), `internal/`, `cmd/`, `migrations/`, `deploy/`, `infra/`,
`charts/`, `.github/workflows/`  
**Method:** every claim below was verified against source, migrations, or manifests — not
inferred from documentation. File and line references are from that commit and will drift.

## How to read this

| Priority | Meaning |
|---|---|
| **P0** | Deployment-blocking, exploitable, or already broken in a deployed environment |
| **P1** | Serious security hardening, or documentation that actively breaks users |
| **P2** | Accuracy drift and operational gaps |

**Severity is deployment-readiness, not live breakage.** ~~The staging and production
environments exist and are reconciled by ArgoCD, but neither carries real users, real
traffic, or real data~~ — **corrected 2026-07-29: neither environment is deployed at all.**
The manifests, Terraform and ArgoCD Applications are all committed, but nothing has been
applied to AWS. There are likewise no external SDK consumers — the same premise ADR 0003
relies on to justify a clean break with no compatibility window. So "P0" here means *this
will fail the moment the environment is stood up and carries load*, not *customers are
affected right now*. Nothing below is downgraded on that basis: pre-production is the cheap
time to fix all of it, and the absence of a deployment is why several of these have gone
unnoticed rather than a reason to defer them.

That correction cuts both ways, and it is why the remediation order was revised. Several
findings are *cheaper now than they will ever be again* — changing a VPC CIDR (38) is a
one-line edit today and an environment rebuild once applied; EKS envelope encryption (5) is
irreversible after cluster creation. Those acquire a deadline the rest do not have. Equally,
any finding whose severity rested on "already broken in a deployed environment" — notably 8
and 10 — was mis-rated, because there is no deployed environment for it to be broken in.

Findings marked **[code]** are cases where the documentation correctly describes the
intended behaviour and the implementation does not deliver it — the fix belongs in code,
not prose. Findings marked **[docs]** are the reverse.

**This is a living document.** Finding numbers are stable and are never reused or
renumbered, so earlier references stay valid. As findings are withdrawn or fixed they are
marked in place — **WITHDRAWN** for a false positive, **RESOLVED** for a landed fix, with
the resolving commit — rather than deleted. A resolved finding that leaves residue spawns a
new numbered finding rather than quietly widening its own scope.

---

## P0 — Security, critical

**1. NATS is completely unauthenticated and unencrypted.** `internal/messaging/nats.go:40`
is a bare `nats.Connect(url)` — no token, NKey, credentials, or TLS, and no config field
exists to supply one. The deployment starts NATS with no auth either
(`deploy/k8s/base/infra/nats.yaml:23-30`), and monitoring port 8222 is reachable from every
pod in the namespace (`overlays/*/network-policies/allow-nats.yaml:32-36` uses
`podSelector: {}`). Anyone with network reach can publish to `notification.send` to forge
notifications, or subscribe to `delivery.*` to read every recipient address and rendered
body in flight. This is the trust boundary of the entire pipeline.

**2. ~~API keys are not scoped to a tenant.~~ WITHDRAWN — this is the intended design.**
The app, not the organization, is the isolation boundary; an app legitimately sends on
behalf of many organizations, and organizations span apps. Scoping keys to an organization
would break the core use case. See [ADR 0003](../adr/0003-rename-tenant-to-organization.md).
The real defect is the vocabulary that produced this false positive — see finding 42.

**3. The permission system is essentially unenforced.** `auth.RequirePermission`
(`internal/auth/permissions.go:48`) is defined and unit-tested but has **zero** production
call sites. The only enforcement anywhere is three inline `HasPermission` checks in
`internal/admin/handler_apikeys.go:55,90,139` — and those are **fail-open**
(`if key != nil && !auth.HasPermission(...)` passes when the key is nil). Nothing checks
`notifications:send` on the send path, or the template/tenant permissions on their
handlers. Within one app, a key issued narrowly for sending has full access to everything
except API-key management. The existence of the `permissions` column says differential
privilege is intended.

**4. Insecure secret defaults, with a silent overwrite path.** `internal/config/config.go`
defaults `HERMES_JWT_SECRET` to `hermes-jwt-secret` (:57), `HERMES_API_KEY_HMAC_SECRET` to
`hermes-dev-hmac-secret` (:71), `HERMES_CENTRIFUGO_API_KEY` to `centrifugo-api-key` (:59),
and the database URL to `sslmode=disable` (:54) — with no environment gate and no startup
warning, so a service with no env vars comes up fully functional and trivially forgeable.
Worst of the four: `EnsureHermesSigningKey` (`internal/store/postgres/auth.go:90-101`)
upserts the JWT secret with `ON CONFLICT DO UPDATE SET secret`, so one service starting
without the variable silently overwrites a properly rotated signing key.

**5. The EKS API server is public to the entire internet.**
`infra/terraform/modules/eks/main.tf:42-44` sets `endpoint_public_access = true` with
`public_access_cidrs` defaulting to `["0.0.0.0/0"]` (`modules/eks/variables.tf:47-51`), and
the root module never overrides it — both environments. Compounded by no control-plane
audit logging (no `enabled_cluster_log_types`) and no KMS envelope encryption for etcd
secrets (no `encryption_config`).

**6. The Crossplane IAM role is account-wide admin over data services.**
`infra/terraform/modules/eks/main.tf:335-347` grants `rds:*`, `elasticache:*`,
`secretsmanager:*`, and `ssm:*` on `Resource = "*"`. `secretsmanager:*` on `*` means the
in-cluster provider can read or overwrite every secret in the account. Compare ESO, which
does far less and is correctly scoped to `secret:hermes/*` (same file, line 178).

**7. Production Crossplane authenticates as staging.**
`infra/crossplane/provider/runtime-config.yaml:11` hardcodes
`arn:aws:iam::471524413120:role/hermes-staging-crossplane`, and that one file is applied to
every cluster. Relatedly, `infra/scripts/bootstrap-cluster.sh:14` *requires* a
`CROSSPLANE_ROLE_ARN` argument that the script then never uses — the value
`deployment-guide.md:164` tells operators to pass is silently discarded.

## P0 — Reliability, critical

**8. `hermes-send` is unreachable in staging and production.** Default-deny covers all pods
(`network-policies/default-deny.yaml:7`), and `allow-ingress.yaml:12-16` lists only
`hermes-admin`, `hermes-inbox`, `hermes-user`, and `centrifugo` on ports 8080/8086/8087/8000
— no `hermes-send`, no port 8088. The ingress routes `/v1/send` to it
(`base/ingress.yaml:13-19`), so the platform's primary write path is blocked by
NetworkPolicy in both environments. `hermes-send` is also absent from the replicas patch,
resources patch, HPA set, PDB set, anti-affinity patch, and the Kargo health check — so it
also stays at `replicas: 1` in production.

**9. [code] Delivery failures are acked, never retried or dead-lettered.**
`internal/delivery/worker.go:62-67` logs the provider error, publishes a `<channel>.failed`
event, and returns `nil`, which the messaging layer treats as success and acks
(`internal/messaging/nats.go:260`). The retry/backoff and DLQ machinery in
`internal/messaging` is therefore dead code for all three delivery workers, and a transient
SMTP or webhook blip permanently drops the notification. Same pattern for unmarshal errors
at :41-43. This directly contradicts the documented DLQ behaviour.

**10. All OpenTelemetry egress is blocked.** Services target
`otel-collector-…observability.svc:4317` (`base/kustomization.yaml:27`), but the egress
rules permit only DNS, NATS 4222, Centrifugo 8000, 5432/6379 into `10.0.0.0/8`, and 443 to
public IPs. Port 4317 to the `observability` namespace is denied, so traces and metrics
never leave the pods. No policy allows Prometheus in `observability` to scrape Hermes
either, so the ServiceMonitors do not work.

**11. The migration Job cannot re-run, and will break every Kargo promotion.** The ArgoCD
hook annotations in `deploy/k8s/base/migration-job.yaml:5-8` are commented out behind a
`TODO: re-enable once Crossplane has provisioned the database`, so the Job is a plain
resource that runs once at first sync and never again — `deployment-guide.md:472`'s claim
that migrations run on every sync is false. Worse, Kargo rewrites the `hermes-migrate` image
tag on each promotion (`kargo/stages/production.yaml:63-65`) but `Job.spec.template` is
immutable, so ArgoCD sync fails with a field-immutable error instead.

**12. The secrets bundle composition is a no-op.** `compositions/aws/secrets.yaml` creates
an *empty* Secrets Manager secret plus two SSM parameters and emits no `connectionDetails`,
though the XRD declares seven keys including `database_url` and `redis_url`
(`xrds/hermes-secrets.yaml:13-20`) and the required `databaseSecretRef`/`cacheSecretRef`
inputs are never referenced. The secret ESO reads is never populated, so every
ExternalSecret key fails to resolve until someone fills it in by hand — a manual step absent
from the guide, which instead claims the composition assembles connection details
(`deployment-guide.md:438`).

## P1 — Security, high and medium

**13. SMTP silently disables TLS when credentials are absent.**
`internal/email/smtp.go:28-36` sets `mail.WithTLSPolicy(mail.NoTLS)` in the no-credentials
branch rather than falling back to opportunistic STARTTLS. Defaults target a local MailHog,
but any deployment pointing `HERMES_EMAIL_SMTP_HOST` at a real relay without credentials
sends notification bodies in cleartext.

**14. No TLS configuration for any datastore.** Postgres depends entirely on the URL
(defaulting to `sslmode=disable`), Redis likewise, and NATS not at all. No config surface
exists to enable it.

**15. GitHub Actions OIDC trust is scoped to the whole repository.**
`infra/terraform/modules/cicd/main.tf:36-38` uses `StringLike` on
`repo:${org}/${repo}:*`, so any workflow on any branch or tag can assume
`hermes-github-actions` and push to ECR — and Kargo promotes whatever lands in ECR. Scope to
`refs/heads/main` plus the release tag pattern, or use a GitHub Environment claim.

**16. No Action pinned to a commit SHA, and no image signing.** `.github/workflows/ci.yml`
and `cd.yml` use mutable tags (`actions/checkout@v6`, `docker/setup-buildx-action@v4`,
`aws-actions/configure-aws-credentials@v6`, `golangci/golangci-lint-action@v9`) in workflows
holding `id-token: write` and ECR push rights. No cosign signing, no SLSA provenance, and no
admission-time verification, so nothing downstream can prove an image came from CD.

**17. Placeholder Centrifugo secrets committed.**
`deploy/k8s/base/infra/centrifugo.yaml:12-13` ships
`"token_hmac_secret_key": "CHANGE-ME-must-match-HERMES_JWT_SECRET"` and
`"api_key": "CHANGE-ME-centrifugo-api-key"`. Staging and production overlays do patch these,
so real environments are not exposed — but any deployment of `base` without the patch uses
known constants, allowing forged Centrifugo JWTs and subscription to any `user#<id>` channel.

**18. ArgoCD runs with TLS disabled and no project isolation.**
`infra/scripts/bootstrap-cluster.sh:76` passes `server.extraArgs={--insecure}`, and both
Applications use `project: default` (`deploy/argocd/production.yaml:17`), permitting any
destination cluster, namespace, and resource kind. The script also echoes the ArgoCD and
Kargo admin passwords to stdout (lines 78, 91), landing them in shell history and CI logs.

**19. NATS and Centrifugo pods have no securityContext.** Every application Deployment sets
`runAsNonRoot`, `runAsUser: 65534`, `seccompProfile: RuntimeDefault`,
`readOnlyRootFilesystem`, and `drop: ["ALL"]` (e.g. `base/services/admin.yaml:21-27,53-57`).
Neither infra manifest has any of it, so both run as root with default capabilities. The
`hermes` namespace also carries no Pod Security Admission labels (`base/namespace.yaml`).

**20. Two JWT validation hardening gaps.** The per-key `Algorithm` field reaches
`JWTSigningConfig` (`internal/auth/signing_config.go:17`) but is never enforced during
validation — only the HMAC family is checked (`internal/auth/jwt.go:72-74`) — so a key
registered HS512 accepts an HS256 token signed with the same secret. Separately, the
missing-claims check at `jwt.go:99` is `if userID == "" || (!tok && tenantID == "")`; when
the tenant claim key is present but not a string, `tok` is true while the conversion yields
`""`, and the request proceeds with an empty organization ID in context.

**21. Lower-severity security items.** Raw API keys used as in-memory rate-limiter map keys
(`internal/send/server.go:91-93`); `automountServiceAccountToken` not disabled
(`base/serviceaccount.yaml`) though no service needs K8s API access; `force_delete = true`
on production ECR repositories (`modules/ecr/main.tf:24`); staging secrets bundle
`recoveryWindowDays: 0` (`claims/staging/secrets.yaml:11`, and the XRD default at
`xrds/hermes-secrets.yaml:40`) making a deleted secret unrecoverable; Datadog ClusterRole
grants `nodes/proxy` (`base/datadog/rbac.yaml:12`) = kubelet API access on every node; no VPC
flow logs (`modules/vpc/main.tf`); a personal email committed as the ACME contact
(`bootstrap-cluster.sh:49`); ESO policy grants `GetSecretValue` but not `DescribeSecret`,
which ESO also calls.

## P1 — Documentation that actively breaks users

**22. [docs] `self-hosting/configuration.md` documents a Helm schema the chart rejects.**
Four keys are wrong, verified against `charts/hermes/values.yaml`: `services.admin` (:138-164)
vs. top-level (`values.yaml:79`); `replicaCount` (:140) vs. `replicas` (`values.yaml:81`);
`networkPolicies` (:278) vs. `networkPolicy` (`values.yaml:339`); and `ingress.host` (:186),
which does not exist in the chart at all — the ingress reads `global.domain`
(`templates/ingress.yaml:18`). `self-hosting/production.md` is correct on all four. Since
`configuration.md` is the doc titled "Configuration Reference" and linked from
`docs/README.md:59`, it is the more damaging error, and following it produces an install
that fails validation.

**23. [docs] Webhook email delivery does not exist but is documented in three places.**
`self-hosting/configuration.md:52-76` presents `provider: webhook` with a
`hermes.email.webhook.url` block as the way to use both SES and SendGrid. The code supports
only `smtp` and `ses` (`internal/email/email.go:40-49`, which errors "unknown email
provider"), and the chart's own schema rejects `webhook` (`values.schema.json:66`) — so the
example fails at install time. SES is natively supported via `provider: ses`, which that doc
never mentions. `deployment-guide.md:251-256` has the mirror-image problem, instructing
operators to create an `email_webhook_url` SSM parameter no Go code reads (the plumbing at
`deploy/k8s/overlays/*/external-secrets.yaml:63` is dead config). The adjacent
`sms_webhook_url` at :258-259 is correct — SMS genuinely is webhook-based.

**24. [docs] `integration-guide.md` API shapes and URL prefixes are stale.** Migration
`000016` dropped the flat template content columns and `users.email`/`users.phone`:
- Templates now take nested `content: {"email": {"subject": …, "body": …}}`
  (`internal/admin/handler_templates.go:20`, `api/admin/openapi.yaml:126-142`), not the flat
  `email_subject`/`inbox_body` fields shown at :144-148. A repo-wide grep for those JSON
  tags returns zero hits. Same stale model in `data-model.md:47-50`, `glossary.md:25`,
  `cli.md:44`.
- User profiles expose `contacts map[string]string` (`internal/models/models.go:27-34`,
  `api/user/openapi.yaml:182-209`), not the flat `email`/`phone` at :352-354.
- Send recipient overrides are `contacts`, not `email`/`phone`
  (`internal/send/handler_send.go:16-20`, `internal/nats/messages.go:16`) — also stale at
  `architecture.md:93`.
- `:158` claims `POST /v1/send` is "served by both the Admin service and the dedicated Send
  service." `internal/admin/server.go:123-130` does not register it. The confusion
  originates in `cmd/openapi/main.go:30-54`, which deliberately merges the send service's
  paths into the admin spec for a combined SDK, so `api/admin/openapi.yaml:972` advertises
  an endpoint the admin binary does not serve.
- Every curl example uses an `/admin/v1/…` prefix; all four services serve at bare `/v1/…`.

**25. [docs] The dual-store reality is documented only inside ADR 0001.** A complete
DynamoDB implementation (~1,500 lines in `internal/store/dynamo/`, with integration tests)
is wired into five services — dispatch, admin, inbox, user, worker-events — gated on
`HERMES_DYNAMO_ENDPOINT`, with `aws-sdk-go-v2/service/dynamodb` a **direct** dependency
(`go.mod:17`). Yet `architecture.md:33,162`, `data-model.md:3`, `services.md:76`, and
`deployment-guide.md:66-88` all describe Postgres as the only store, and
`configuration.md` omits both `HERMES_DYNAMO_ENDPOINT` and `HERMES_DYNAMO_REGION`
(`internal/config/config.go:75-76`). Note Postgres remains required even in Dynamo mode:
`dynamo.NewEventStore` takes the Postgres store as a delegate, and every service still calls
`MustConnectDB` unconditionally.

**Re-verified and scoped 2026-07-29.** All claims hold at current coordinates: `go.mod:17`,
`internal/config/config.go:75-76`, and the five gated call sites (`cmd/admin/main.go:45`,
`cmd/dispatch/main.go:46`, `cmd/inbox/main.go:59`, `cmd/user/main.go`,
`cmd/worker-events/main.go:39`, plus `cmd/dispatchbench/wiring.go`). A case-insensitive
search for "dynamo" across `docs/` hits only `adr/`, `reviews/`, `superpowers/` and
`loadtest/` — the single `deployment-guide.md:98` hit is about Terraform state locking and
unrelated. The user-facing docs imply the backend does not exist.

**Decision (2026-07-29): the DynamoDB backend is a supported deployment option**, not an
internal experiment. That settles the open question this finding raised and enlarges it from
"mention the dual store" to a documentation workstream:

- `HERMES_DYNAMO_ENDPOINT` / `HERMES_DYNAMO_REGION` in `self-hosting/configuration.md`, with
  the constraint that **Postgres remains required regardless** — it is a delegate, not an
  alternative.
- The dual-store reality in `architecture.md` and `data-model.md`, replacing the
  "single shared database" claims at `architecture.md:172` and `data-model.md:3`.
- `internal/store/dynamo` added to the shared-packages table in `services.md:76` (this
  overlaps finding 30, which flags the same table for a different omission — do both at once).
- A backup and restore story for the Dynamo path. `self-hosting/production.md` has no backup
  section at all today, and the AWS DR section at `deployment-guide.md:541-565` is never
  referenced from the self-hosting path (this is finding 40's second gap; the two should land
  together).
- **An integrator-facing warning about cursor incompatibility.** ADR 0001:225-232 records
  that Postgres-format cursors are not forward-compatible and that clients get
  `"invalid cursor"` after a backend switch. Searching outside `docs/adr/` and `docs/reviews/`
  for "invalid cursor" returns zero hits — nothing in `integration-guide.md` or any SDK doc
  warns anyone. Now that this is a supported option, that omission becomes a defect in its
  own right rather than an internal note.
- The cleanup CronJob's Dynamo no-op (`cmd/cleanup/main.go:27-30` exits early when
  `HERMES_DYNAMO_ENDPOINT` is set) becomes documented behaviour rather than a surprise —
  and arguably a gap to close, since retention then silently does not run.

## P2 — Documentation accuracy and drift

**26. Service and repository counts contradict each other.** `deployment-guide.md:58,328`
say 8 services; `:74,137,312` say 9. Reality: **9 services** (`base/services/kustomization.yaml`),
**10** ECR repositories and 10 CD images — `migrate` is included (`modules/ecr/main.tf:5-16`,
`cd.yml:31-41`; `warehouse.yaml:1` also says "all 9" while subscribing to 10). Line 328 is
the worst instance because it is an operator verification step, teaching people to accept a
missing pod as healthy.

**27. JWT rotation — `architecture.md:143-145` is the incorrect one.** "Rotated without
downtime" is misleading. Verification is genuinely multi-key from `jwt_signing_keys`, but
`EnsureHermesSigningKey` (`internal/store/postgres/auth.go:90-101`) upserts the
`hermes-internal` row with `ON CONFLICT DO UPDATE SET secret`, so changing
`HERMES_JWT_SECRET` replaces the key rather than adding alongside it — as `configuration.md:47`
and `deployment-guide.md:466` correctly state. Multi-key rotation only helps for manually
inserted third-party keys, and `integration-guide.md:473` requires Centrifugo's
`token_hmac_secret_key` to equal `HERMES_JWT_SECRET`, so Centrifugo cannot validate those
extra keys anyway.

**28. The documented DB password rotation will be reverted by Crossplane.**
`compositions/aws/database.yaml:119-122` sets `autoGeneratePassword: true` with a
`masterPasswordSecretRef`, so the guide's out-of-band `aws rds modify-db-cluster`
(`deployment-guide.md:444-457`) creates drift that the next reconcile resets — breaking the
connection string just written to Secrets Manager. Correct rotation is to rotate the
Crossplane-managed secret and let it propagate. Same trap for webhook URLs:
`compositions/aws/secrets.yaml:62,77` set `insecureValue: "https://REPLACE_ME/email"`, so
the `aws ssm put-parameter` commands at guide lines 254-259 revert on next reconcile.

**29. `data-model.md` omits two shipped tables.** `template_channel_content` and
`user_contact_points` (both migration `000015`) are absent from the text and from the entity
diagram at :14-26, and `:33` still lists `email`/`phone` as columns on `users`. The
`verified` column on `user_contact_points` is written by no code and exposed by no API. The
retention section at :102-106 also does not note that `cmd/cleanup` no-ops entirely under
DynamoDB (`cmd/cleanup/main.go:27-30`), where native TTL handles expiry.

**30. Neither `architecture.md` nor `services.md` reflects ADR 0002.** The shared-packages
table at `services.md:76-91` lists neither `internal/provider` (the channel/provider registry
that shipped with ADR 0002 Phase 1) nor `internal/store/dynamo`. `glossary.md:28` still
defines a channel as "`email`, `sms`, or `inbox`" with no entry for *provider*, which ADR
0002 establishes as a distinct first-class concept, and no human-facing doc explains the
channel/provider distinction. For accuracy: the stream table *is* still correct — the
registry is compile-time only (`internal/provider/builtins.go:10`) and per-provider
`delivery.<channel>.<provider>` subjects do not exist yet.

**31. Assorted stale claims.** `docs/api/README.md:33` says "the three JetStream streams"
(there are four, and its own AsyncAPI documents the DLQ at
`api/async/asyncapi.yaml:107-126`). `self-hosting/upgrading.md:49` says migrations "cannot be
rolled back automatically" though every migration ships a `.down.sql`. The rollback recipe
at `deployment-guide.md:480-485` invokes `/migrate` inside the admin image, which has
entrypoint `/service` and no such binary, and names the job `hermes-migration` rather than
`hermes-migrate`. `deployment-guide.md:500` warns about upgrading 1.31 → 1.32 while the
default `eks_cluster_version` is 1.35. Staging nodes are `t4g.large`
(`environments/staging.tfvars:3`), not `t4g.medium` (:136). "Production services use HPA"
(:420) covers 4 of 9. The `OWNER` repoURL placeholders (:236-243), ACME email (:264), and
staging domain (:207) are already filled in — staging is `staging.hermes.dgr.io`, though
production is still the `hermes.example.com` placeholder. The hardcoded account ID appears
in 5 files, not the 1 claimed at :223. `testing.md:33` and `CLAUDE.md` both cite
`TestCreateGroup`, which no longer exists (renamed in the group→category migration; the
current equivalent is `TestCreateCategory_And_GetBySlug` in
`internal/store/postgres/categories_test.go:13`) and omit `-tags=integration`, so the command
silently matches nothing. `dispatchbench` (`Makefile:248`) is missing from `services.md:61-70`,
`cli.md:88-94`, and `development.md:92-93`. `adr/0001:245` references a "namespace" concept
that exists only as a plan doc. `docs/loadtest/dispatch-tuning-dynamo.md:28` recommends
`workers=16` against a shipped default of 8 with no cross-reference to the superseding
`dispatch-tuning-2026-06.md:136`. Repo identity is inconsistent across `cli.md:10`
(`hermes-notifications`, matching `go.mod:1`), `upgrading.md:5` (`hermesnotifications`), and
`adr/0001:311` (`darylrobbins`).

**32. Channel resolution is documented incorrectly.** The documented "explicit → user
preference → category defaults" is not what `internal/dispatch/channels.go:82-134`
implements. A standalone template resolves explicit → `template.DefaultChannels` → error. A
category with `default_state = "required"` resolves explicit → category defaults, bypassing
user preference entirely. Otherwise the list is the category defaults, overridden *wholesale*
by explicit channels, after which the user's subscription acts purely as a **binary
opt-in/opt-out gate** (:119-125). `models.UserSubscription` has only `OptedIn bool`
(`internal/models/models.go:96-101`) — there is no per-channel user preference in the data
model at all, so preferences can suppress a notification but never select channels.
Narrowing to template-defined channels does happen, but separately and afterwards
(`dispatch.go:269`), followed by contact-availability filtering (:288). Relatedly,
`internal/nats/messages.go` no longer has discrete `email`/`phone` fields (now `Contacts` and
`Recipient` maps), and the docs omit that all four streams carry a 7-day `MaxAge`
(`nats.go:59`) — so "stream retention is 7 days" is the accurate claim.

## P2 — Operational gaps

**33. The cleanup CronJob references an image nobody builds.**
`base/cleanup-cronjob.yaml:23` uses `image: hermes-cleanup`. `cmd/cleanup` exists, but there
is no `hermes-cleanup` ECR repository, it is not in the CD matrix, and Kargo does not set its
tag — so the overlays leave the bare string and it will `ImagePullBackOff` nightly at 03:00
UTC. It also has no resource requests or limits.

**34. ArgoCD `selfHeal` fights the HPAs.** Both Applications enable `selfHeal: true` while
the production overlay commits explicit `replicas` values *and* HPAs for the same four
Deployments, with no `ignoreDifferences` on `/spec/replicas` — so ArgoCD keeps reverting
whatever the HPA scaled to.

**35. The Kargo health check almost certainly cannot pass.**
`kargo/analysis/health-check.yaml` runs `bitnami/kubectl:latest` (unpinned, on a registry
being retired) as a Job in the `hermes` namespace. No Role or RoleBinding granting
`deployments`/`statefulsets`/`pods` read access exists anywhere in the repo, and default-deny
egress blocks the pod from reaching the API server. It also omits `hermes-send`. Production
has no `verification` block at all, so nothing verifies a production promotion.

**36. NATS has a PDB but no anti-affinity.** `pdb/nats-pdb.yaml` sets `minAvailable: 2` for
the 3-replica StatefulSet, but `patches/anti-affinity.yaml` covers only admin, dispatch,
inbox, and user. All three NATS pods can land on one node, so a single node loss takes
JetStream below quorum *and* simultaneously wedges every voluntary eviction. Centrifugo and
the four workers have no spread constraints; PDBs are missing for send, user, and all
workers.

**37. The ECR lifecycle policy never matches anything.** `modules/ecr/main.tf:65` expires
tagged images with `tagPrefixList = ["v", "sha-"]`, but CD tags with a bare 40-character git
SHA and, for releases, a version with the `v` already stripped (`cd.yml:85`). Neither prefix
matches, so the "keep only 20 tagged images" rule is inert and tagged images accumulate
indefinitely.

**38. Both environments use the same VPC CIDR.** `vpc_cidr` defaults to `10.0.0.0/16` and
neither tfvars file overrides it, so the staging and production VPCs overlap and can never be
peered.

**39. Rate limiting is undocumented and not configurable.** `internal/middleware/ratelimit.go`
is live on all four HTTP services with hardcoded limits — send 5000 burst / 2000 per second
keyed on the `Authorization` header (`internal/send/server.go:91-93`), admin 1000/500, inbox
and user 50/20 per user — returning HTTP 429. Nothing in `docs/` mentions rate limits, 429, or
these numbers, so integrators get no documented retry contract. Two properties matter
especially and appear nowhere: the limiter is **in-process with no Redis backing**, so the
effective cluster limit multiplies by replica count (silently interacting with the HPA
guidance at `deployment-guide.md:420-422`), and **no env var tunes it**.

**40. Missing documentation coverage.** PII and retention — only `notification_events`
retention is documented (`data-model.md:102-106`), with no guidance on deleting a user's
data, purging `user_contact_points` (which holds all PII after `000015`), or the fact that
soft-deleted notifications are never hard-deleted, and no right-to-erasure discussion.
Backup and restore for self-hosting — `self-hosting/production.md` has no backup section at
all, the only guidance is a single `pg_dump` line at `upgrading.md:10` with no restore
procedure, and self-hosters are never pointed at the solid AWS DR section at
`deployment-guide.md:541-565`; nothing covers backing up the DynamoDB-model store on either
path. Store-backend migration — ADR 0001:225-233 records that Postgres-format inbox cursors
are not forward-compatible and clients get an "invalid cursor" error after a store switch,
but that client-visible breakage appears nowhere an integrator would look. Chart requirements
— no doc mentions that `global.domain`, `hermes.jwt.secret`, and `hermes.apiKey.hmacSecret`
are hard-required (`values.schema.json:9,31`), or that sub-charts default to `enabled: true`
so a default install is self-contained rather than BYO-infra; the schema enum also accepts
`sendgrid` (:66) though no template handles it, so it validates and silently emits no
provider config.

**41. Assorted infrastructure gaps.** `hermes-config-params` (the SSM webhook URLs) is
created by ESO in both overlays but referenced by no Deployment, so
`HERMES_SMS_WEBHOOK_URL` never reaches the worker. Centrifugo's
`HERMES_CENTRIFUGO_REDIS_PASSWORD` is synced (`overlays/production/external-secrets.yaml:39-42`)
but never wired into the pod env by `patches/centrifugo-env.yaml`, so it cannot authenticate
to Valkey given `autoGenerateAuthToken: true` (`compositions/aws/cache.yaml:108`). The
ElastiCache composition sets `automaticFailoverEnabled` but never `multiAzEnabled`, so
production Valkey is not actually multi-AZ. The Aurora composition exports no CloudWatch
logs. The Helm chart's own NetworkPolicy (`charts/hermes/templates/networkpolicy.yaml`)
specifies `ports` with no `to:` selector on egress and no `from:` on ingress, making its
"default deny" far more permissive than the Kustomize equivalent — it allows egress to any
destination and ingress from any pod in any namespace on those ports.

## P1 — Vocabulary (added after review)

**42. ~~The docs assert an isolation model that does not exist, and it produced a false
positive.~~ RESOLVED at `a5f0874`.** Migration `000017` renames `tenants` →
`organizations`, both `tenant_id` columns, the two indexes, the two FK constraints, and
`jwt_signing_keys.tenant_id_claim` → `organization_id_claim` (backfilling existing rows and
moving the column default to `organization_id`). `internal/nats/messages.go:14,44` carry
`OrganizationID`, the Dynamo idempotency prefix is `ORG#`
(`internal/store/dynamo/notifications.go:107,144`), `handler_tenants.go` became
`handler_organizations.go`, the admin spec serves `/v1/organizations`
(`api/admin/openapi.yaml:972`), and `web/admin` and the k6 fixtures are clean. The two false
isolation claims were corrected, an *app* glossary entry added, and the app-scoped API key
model written up in the architecture auth section — discharging the Docs row of the table
below, including the isolation-model section it owed. `docs/adr`, `docs/reviews`,
`docs/superpowers`, and migrations `000001`–`000016` were deliberately excluded as
append-only records.

Three residues remain, none of which reopens the finding:

- The four generated SDKs were not regenerated — **finding 43**.
- The metric-label rename shipped inside the same commit rather than separately — **finding 44**.
- ADR 0003 is still `Status: Proposed` in both the ADR and `docs/adr/README.md:15`. The
  review's own follow-up said to flip it to `Accepted` in the PR that lands the rename; that
  PR landed and it was not flipped.

The original finding is preserved below as the record of why the rename happened.

**Original finding.** `glossary.md:6` calls the tenant "the top-level isolation boundary" and
`integration-guide.md:78` says tenants "represent isolated organizations." Both are false:
the app is the boundary, organizations deliberately span apps, and API keys are
intentionally unscoped. Read against the schema, those two sentences describe a
vulnerability that isn't one — which is exactly the error finding 2 made. The inverse risk
is worse: someone assuming the isolation exists and building on it, or "fixing" the
non-issue by scoping keys and breaking cross-organization sends. Corroborating evidence that
the name is the only thing wrong: only `users` and `notifications` carry `tenant_id` at all;
the entire config plane (`subscription_categories`, `subscriptions`,
`notification_templates`, `jwt_signing_keys`) is app-global. Decision recorded in
[ADR 0003](../adr/0003-rename-tenant-to-organization.md).

**Remediation is not a documentation edit.** ADR 0003 decides a clean rename of `tenant` →
`organization` with no compatibility aliases, plus naming the app as the boundary. That
spans roughly 130 files (the commit touched 148):

| Area | Work |
|---|---|
| Schema | Migration renaming `tenants` → `organizations`, `users.tenant_id` and `notifications.tenant_id` → `organization_id`, and the two indexes. Precedent: `migrations/000011` |
| JWT | `jwt_signing_keys.tenant_id_claim` → `organization_id_claim`, default `tenant_id` → `organization_id` |
| Wire contracts | `SendMessage.TenantID` / `DeliveryMessage` → `OrganizationID` (`internal/nats/messages.go`), `api/async/asyncapi.yaml` regenerated |
| DynamoDB | `TENANT#<id>#IDEM#<key>` → `ORG#…` (`internal/store/dynamo/notifications.go:107,144`); safe because entries expire in the 24h dedup window |
| REST + SDKs | `tenant_id` → `organization_id` in the send body; `/v1/tenants` → `/v1/organizations` (`internal/admin/handler_tenants.go:41,73`); `Tenant*` → `Organization*` across the Java, Python, .NET, and TypeScript SDKs; major version bump |
| Docs | The false claims above, a new *app* glossary entry, and the missing isolation-model section from finding 40 |
| Observability | Four overlay files reference tenant as a **metric label** — renaming silently breaks saved queries, dashboards, and any alert rule grouping by it. Needs a coordinated dashboard update, not a find-replace. Ship as a separate reviewable commit |
| Other | `cmd/loadseed` fixtures and manifests, `web/admin` |

**Sequencing constraint — this has a deadline, not just a priority.** ADR 0003 argues the
rename must land *before* two planned changes, after which the cost rises sharply: ADR 0001
Phase 2 would bake `TENANT#<id>` into DynamoDB GSI **partition** keys (converting a spec
regeneration into a stored-key migration), and ADR 0002's subject-contract freeze would make
the old name part of a public versioned surface that third-party provider authors depend on.
Schedule it ahead of both regardless of its P1 ranking.

*Constraint met.* The rename landed ahead of both, so neither escalation occurred. The
`ORG#` prefix is in place before Phase 2 promotes it to a GSI partition key, and the
`delivery.*` subject family carries the new name before the ADR 0002 freeze.

## Added 2026-07-27 — residue from the rename

Both findings below are consequences of `a5f0874`. They did not exist at `68f0996`.

**43. ~~[P1] [docs] All four SDKs still describe the pre-rename API, including an endpoint the
server no longer serves.~~ RESOLVED 2026-07-28.** All four SDKs regenerated from the current
specs; `grep -ri tenant sdks/` now returns zero hits. Across the three generated SDKs it was
a 1:1 replacement — 27 `Tenant*` files removed, 27 `Organization*` files added, 84 modified.
`admin-api.d.ts:93` declares `/v1/organizations`, and the client SDKs type
`organization_id`. Verified green: `tsc --noEmit` on both TypeScript packages, `mvn test` on
Java, `dotnet test` on .NET (240 passed / 0 failed / 28 skipped), and a syntax parse over
every generated Python module.

Two things worth recording for the next regeneration:

- **The generator never deletes.** It only writes, so the orphaned `Tenant*` sources, docs,
  and tests survived regeneration and had to be removed by hand. Worse, it logs `Test files
  never overwrite an existing file of the same name` and skips every existing test stub — so
  21 stubs still said `tenantId` after a clean regeneration. They had to be deleted and the
  generators re-run. Budget for this on any future rename: regeneration alone is not
  sufficient.
- **The stubs were two changes stale, not one.** The regenerated
  `test_send_recipient.py` picked up `contacts` alongside `organization_id` — the old stub
  still listed `email`/`phone`, which migration `000016` replaced. They had been frozen since
  before the rename, which is why the .NET count moved 242 → 240.

The mechanism that let this drift is now its own finding — see **45**. The remaining ADR
0003 obligation is the major version bump, which is a release decision rather than a code
change. Original finding follows.

**Original finding.** `make openapi` regenerated the specs in the rename commit, but
the SDKs generated *from* those specs were not. The result is worse than stale naming — the
generated clients are now wrong about the live API:
`sdks/typescript/packages/hermes-server/src/generated/admin-api.d.ts:218` still declares the
`/v1/tenants` path with `list-tenants`/`create-tenant` operations, while
`internal/admin/server.go` registers only the organizations routes and
`api/admin/openapi.yaml:972` documents `/v1/organizations`. Any consumer of the generated
client calls a path that now 404s.
`sdks/typescript/packages/hermes-client/src/generated/user-api.d.ts:201` and
`inbox-api.d.ts:197` still type the field as `tenant_id`, which no longer appears in any
response. Java, Python, and .NET are worse off still, carrying whole `Tenant*` types and API
classes — `TenantsApi`, `TenantItem`, `CreateTenantInputBody` and their tests (32, 33, and
32 files respectively; TypeScript, 3).

The commit message excluded them deliberately, on the stated grounds that "the generated
Java/Python/.NET/TypeScript SDK artifacts are regenerated from the specs rather than
hand-edited" — correct as a method, but the regeneration step itself never ran, so the
exclusion silently became a gap. ADR 0003 commits to regenerating all four and taking a
major version bump. Nothing is published to a registry yet, so the fix is to run the
generators and bump; the cost of leaving it is that the checked-in SDKs are an actively
misleading reference for the exact audience the rename was meant to protect.

**44. [P2] The metric-label rename shipped in the bulk commit, and out-of-repo dashboards
were not updated.** `tenant_id` → `organization_id` at
`deploy/observability/base/otel-collector/values.yaml:92` and the four SigNoz exporter
overlay patches. No `tenant_id` remains anywhere in `deploy/`, `charts/`, or `infra/`, so the
in-repo side is complete and consistent — but ADR 0003 specifically called this out as the
one part of the rename carrying real risk, and asked for it as **a separate reviewable
commit with a coordinated dashboard update**. It shipped inside the 148-file mechanical pass
instead, where a reviewer is least likely to weigh it, and the coordinated update did not
happen.

~~Grafana dashboards, saved queries, and any alert rule that groups by the old label live
outside this repo and still reference `tenant_id`. They do not error; they silently return
no series. Alert rules are the sharp edge — one grouping by `tenant_id` stops firing rather
than starts failing, and the pre-production quiet makes that indistinguishable from healthy.
Audit the LGTM dashboards and alert rules for the old label.~~

**Amended 2026-07-29 — the premise above was wrong, and the process point survives without
it.** The renamed token was never an emitted metric label. All five occurrences are entries
in an `attributes/metrics` **deletion** list —
`deploy/observability/base/otel-collector/values.yaml:85-93`, under the comment "Strip
high-cardinality attributes before they hit Prometheus (they remain on spans)" — and that
processor sits in the metrics pipeline (`values.yaml:136-139`), the only pipeline feeding
`prometheusremotewrite` and, via overlay, `datadog`. `docs/observability/semantic-conventions.md:23-29`
independently lists `organization_id` under "Forbidden high-cardinality labels — These kill
Prometheus. Never put them on metrics," and calls the collector rule a backstop. No Go code
emits such an attribute: the only `attribute.String` call sites in `internal/` are `stream`,
`consumer`, `reason` (`internal/messaging/dlq.go:56-64`) and `batch.size`,
`messaging.destination` (`internal/eventwriter/writer.go:79-80`).

So no Prometheus series has ever carried `tenant_id`, and a panel or alert grouping by it
returned no series *before* the rename too. The stated failure mode — alert rules silently
ceasing to fire — could not have occurred.

The claim that dashboards and alert rules live outside this repo is also wrong. Four
dashboards (`deploy/observability/base/grafana/dashboards/`) and three rule files
(`deploy/observability/base/prometheus-rules/`) are checked in, and grepping all seven for
`tenant`/`organization` returns zero hits — every expression groups by bounded labels only
(`sum by (channel)`, `sum by (stream, consumer)`, `sum by (service)`, `sum by (le)`). The
in-repo alerting surface was never exposed and needs no fix.

**What is actually left, and it is not what the finding asked for.** The production and
staging Datadog collector config is *not* checked in —
`deploy/observability/overlays/production/patches/otel-collector-dd-exporter.yaml:2-4` states
that the pipeline fan-out "is handled by swapping in a production values.yaml for the chart."
Verify that file still deletes `organization_id` and still has `attributes/metrics` in its
metrics pipeline. If it says `tenant_id`, the cardinality guard there is a **no-op** — which
is a cost and cardinality risk, the inverse of the risk originally filed, and the only
actionable item in this finding.

The ADR 0003 process point stands on its own and is the durable lesson: a change ADR 0003
singled out as carrying real risk shipped inside a 148-file mechanical pass where no reviewer
would weigh it. That it turned out to be harmless is luck, not diligence. Keep this as the
worked example for the next rename touching observability config.

## Added 2026-07-28 — found while fixing 43

**45. [P1] Nothing verifies the SDKs against the specs, and the generation toolchain is
neither documented nor pinned.** Finding 43 was not a one-off slip; it is what the current
setup permits by default.

*No CI gate.* Neither `.github/workflows/ci.yml` nor `cd.yml` runs `openapi-check` or any
`sdk-*` target. The specs and the SDKs generated from them can diverge arbitrarily far —
here, a full rename plus an endpoint path — and every check stays green. `make openapi`
followed by `git diff --exit-code api/` confirms the specs were current the whole time, so a
`make sdk-generate && git diff --exit-code sdks/` gate would have caught this on the rename
PR itself.

*Undocumented toolchain.* `CONTRIBUTING.md:10` lists prerequisites as "Go, Docker; for the
k8s dev loop: k3d, tilt, kubectl; pnpm for the portal." Nothing mentions that
`sdk-python`, `sdk-java`, and `sdk-dotnet` all require a **JVM** — `openapitools.json:5`
pins `@openapitools/openapi-generator-cli` 7.20.0, a Node wrapper around a Java JAR. A
contributor with the documented prerequisites installed cannot run two thirds of the SDK
targets, and the failure gives no hint that Java is the missing piece. Building or testing
the output additionally needs Maven and the .NET 8 SDK, also undocumented.

*`make sdk-ts-generate` is broken on current pnpm.* Under pnpm 11.17 it fails with
`ERR_PNPM_IGNORED_BUILDS` on `sharp`, `protobufjs`, and `unrs-resolver` — all `web/admin`
dependencies, none of which type generation touches. pnpm 11 re-runs `install` before every
script and treats unapproved build scripts as fatal; the nested install inherits neither
`--config.strictDepBuilds=false` nor `npm_config_verify_deps_before_run=false`, so the
Makefile target cannot be coaxed into working. The TypeScript SDK was regenerated by
invoking `openapi-typescript` directly. Root cause is that no pnpm version is pinned
anywhere — no `packageManager` field in any `package.json`, and `pnpm-workspace.yaml`
declares no `onlyBuiltDependencies` allowlist.

Remediation is three small changes plus one CI job: declare `onlyBuiltDependencies` in
`pnpm-workspace.yaml`, add a `packageManager` pin, list the JVM/Maven/.NET prerequisites in
`CONTRIBUTING.md`, and add the spec-and-SDK drift gate. The gate is the one that matters —
the others only make it runnable.

## Added 2026-07-28 — found while fixing the admin build

**46. [P1] The entire TypeScript surface has no tests and no test runner.** All five
packages in the pnpm workspace — `web/admin`, and the four SDK packages
`hermes-server`, `hermes-client`, `hermes-react`, `hermes-web` — have **no `test` script**,
and a repo-wide search finds **no test-runner dependency at all**: no vitest, no jest, no
`@testing-library/*`, no Playwright, in any `package.json`. There is nothing to run, so
there is nothing to write a test into.

Rated P1 on the same reasoning as finding 45: this is a *detection* gap, and detection
gaps are what let the other findings survive. It is not hypothetical — it concealed a
real defect for as long as the code has existed. `next build` for the admin portal failed
on any machine without `HERMES_API_URL`/`HERMES_API_KEY`, because every route under
`app/(dashboard)` was being statically prerendered despite being authenticated and
per-request. Nothing caught it, because `ci-web.yml` ran lint and typecheck only and the
production build had never been executed anywhere. Fixed at `bcac3ad` with
`dynamic = "force-dynamic"` on the dashboard layout, plus the build added to CI — but the
fix is one route group, and the reason it went unseen is unchanged.

The documentation does not overclaim here, which is worth stating precisely: `docs/testing.md`
does not mention TypeScript, `web/admin`, or the SDKs anywhere, and `CLAUDE.md:100-105`
describes a strategy entirely in Go terms (`*_test.go`, build tags, `httptest`, mock store
interfaces). Nothing asserts coverage that does not exist. The gap is that no document
acknowledges the JS/TS surface as something that *should* be tested, so its absence never
reads as missing.

Scope note on the SDK packages: `hermes-server` and `hermes-client` are thin generated-type
wrappers where `tsc --noEmit` genuinely carries most of the weight, so the argument for unit
tests there is weaker. `hermes-react` and `hermes-web` ship runtime behaviour — hooks and a
web component — and `web/admin` is a privileged console that creates and revokes API keys.
Those three are where the absence actually costs something.

Remediation is a runner plus a first test, not a coverage campaign: add vitest to the
workspace, a `test` script per package, wire it into `ci-web.yml`, and start with the
behaviour that has already broken. Until a runner exists, the TDD gate cannot be satisfied
in this part of the tree at all — `.claude/guardrails.json` currently exempts
`web/admin/app/**/layout.tsx` for exactly that reason, and that exemption should be removed
once there is something to run.

**Resolved 2026-07-28.** vitest 4.1.10 added to `web/admin`, `hermes-react` and `hermes-web`,
with a `test` script each and both jobs in `ci-web.yml` running it. 51 tests: `relativeTime`
boundaries and `slugify` (admin), `useHermesInbox` against a hand-written fake client
(react), and the `<hermes-inbox>` component in jsdom (web). No vitest config files were
needed — CLI flags cover it — so nothing new was added to the TDD exemption list.

Two corrections to this finding as written. First, the closing recommendation to drop the
`layout.tsx` exemption was only half right: that exemption rested on two reasons, and only
the "no runner exists" one has gone away. Route segment config is still declarative
build-time convention whose whole effect is on `next build`, so the exemption stays, with
its comment rewritten to rest on that alone. Removing it would force a test asserting a
constant equals itself — the fake coverage this finding objects to.

Second, the scope note above is wrong about which packages were drifting. `hermes-client`
compiles clean; it was `hermes-react` and `hermes-web` that **did not build at all** on
`main`, both constructing a `Notification` with `group_id` after the schema moved to
`category_id` (PR #43's normalized model). `ci-web.yml` built only `@hermes-notifications/server`
and blamed the exclusion of the other three on hermes-client, so two genuinely broken
packages sat behind a stale `TODO(#36)` with CI green. Fixed here; CI now builds all four
and type-checks them including their test files, via a `tsconfig.build.json` split that
keeps tests out of the published `dist/`.

Finding 45's remaining half is also closed in passing, because it blocked this work: adding
a dependency forced a choice of pnpm, and local corepack (11.17.0) and CI (10.28.2) produced
different lockfiles. A root `package.json` now pins `packageManager: pnpm@10.28.2`.

## Added 2026-07-29 — found during the triage sweep

**47. [P0] The `part-of: hermes` label never reaches any pod, so four NetworkPolicies select
nothing.** `deploy/k8s/base/kustomization.yaml:6-9` uses the kustomize `labels:` transformer
with `includeSelectors: false` and no `includeTemplates`, so
`app.kubernetes.io/part-of: hermes` is applied to resource `metadata.labels` only — never to
`spec.template.metadata.labels`. Rendering both overlays with `kubectl kustomize` confirms it:
every pod template in staging and production carries only `app.kubernetes.io/name` and
`app.kubernetes.io/component`.

Four of the seven rendered NetworkPolicies key their `podSelector` on `part-of: hermes` —
`allow-egress-managed-services`, `allow-egress-to-nats`, `allow-egress-to-centrifugo`, and the
source selector inside `allow-nats-client`. All four therefore select **zero pods**. Only
`default-deny-all` (`podSelector: {}`) and the two name-keyed policies match anything. The
net declared effect is that every Hermes pod gets DNS egress and nothing else: no Postgres,
no Redis, no NATS, no Centrifugo, no webhooks, no OTLP.

This **subsumes findings 8 and 10**, which describe two specific holes in a policy set that
does not select its pods at all. Fixing 8 or 10 without fixing this changes nothing; fixing
this without first correcting every allow rule flips the namespace from "policies inert" to
"policies enforced and wrong".

Critically, it also explains why the platform works despite 8 and 10:
`infra/terraform/modules/eks/main.tf:370-375` declares the `vpc-cni` addon with no
`configuration_values`, so `enableNetworkPolicy` is unset — and AWS VPC CNI does not enforce
NetworkPolicy unless it is explicitly enabled. **The policies are almost certainly not
enforced at all.** The review's framing of 8 and 10 as "broken in both deployed environments
right now" is therefore wrong; the real hazard is that enabling enforcement, or fixing this
label, takes the entire namespace dark in a single step.

**48. [P1] The Helm chart's ingress omits four admin routes and serves two that no handler
answers.** `charts/hermes/templates/ingress.yaml:28,35` route `/v1/types` and `/v1/groups` —
pre-rename paths no admin handler serves — while the chart has **no** rule for
`/v1/templates`, `/v1/apikeys`, `/v1/organizations`, or `/v1/subscriptions`. Those admin
endpoints are unreachable through a chart install. `deploy/k8s/base/ingress.yaml` has the
missing four but also retains the two dead ones. This is residue of the rename that finding
42's sweep did not reach, and it only affects self-hosters, which is why nothing caught it.

**49. [P1] `sendgrid` is a valid chart value that guarantees a crash at runtime.**
`charts/hermes/values.schema.json:66` declares `"enum": ["smtp", "ses", "sendgrid"]`, and
that enum *is* enforced — so `provider: sendgrid` passes `helm install` validation. But
`charts/hermes/templates/configmap.yaml:22,31` branches only on `smtp` and `ses`, so the
pod starts with `HERMES_EMAIL_PROVIDER: sendgrid` and no provider config, and
`internal/email/email.go:47` then kills the worker with `unknown email provider: "sendgrid"`.
A validating schema that admits a value the code rejects is worse than no schema: it converts
a config error into a runtime crash loop. Either implement the provider or drop it from the
enum.

---

## Triage 2026-07-29 — all open findings re-verified

Every open finding was re-checked against the tree by six parallel read-only agents,
partitioned by domain (security, cloud infrastructure, Kubernetes, application code,
documentation, observability). Each finding was re-anchored to current coordinates rather
than trusted, per the warning in the remediation order below. Method note: verdicts were
`STILL-VALID` / `ALREADY-FIXED` / `PARTIALLY-FIXED` / `CLAIM-WRONG` / `CANNOT-VERIFY-FROM-REPO`,
with a quoted decisive line required for each.

**Standing fact that governs all effort estimates: nothing is deployed yet.** Neither the
staging nor the production environment exists in AWS as of 2026-07-29. This collapses the
cost of several findings by an order of magnitude and should be exploited before it stops
being true:

- **38 (shared VPC CIDR)** is a one-line edit per tfvars file today. Once deployed it is an
  environment rebuild — `aws_vpc.cidr_block` is `ForceNew`, taking the VPC, subnets, NAT
  gateways, EKS cluster, node groups and every attached Aurora/ElastiCache with it.
- **5 (EKS KMS envelope encryption)** is free now and **irreversible** once a cluster exists.
- **6, 7, 12** carry no reconciliation or coordination risk while there is nothing to
  reconcile. In particular 7's fix, which would otherwise make production Crossplane begin
  provisioning real datastores for the first time, is inert today.
- **47 / 8 / 10** — the whole NetworkPolicy cluster can be fixed in one pass and validated
  with `kubectl kustomize` plus a policy unit test, rather than staged behind a soak.

Aggregate: of the ~41 open findings, the overwhelming majority verified as still valid at
corrected coordinates. Four were partially fixed, and several individual claims were refuted.
No finding recorded as resolved was found to be unresolved — 42, 43, 45 and 46 all hold up,
46 verified by executing its suites and 45 by an observed green CI run (`sdk-drift`, run
30323615986, on `68308ad`).

### Claims refuted or corrected

| Finding | Correction |
|---|---|
| **3** | Main claim holds — `auth.RequirePermission` still has **zero** production call sites, and the only enforcement is three inline checks in `handler_apikeys.go:55,90,139`. But the fail-open sub-claim is wrong: `internal/auth/middleware.go:30-34` returns 401 when validation yields nil, so `key == nil` is reachable only via `SetSkipAuth(true)`, which has no non-test caller. Latent hazard, not an exploitable one. |
| **10** | The ServiceMonitor sub-claim is wrong — there are no Hermes ServiceMonitors in the deployed kustomize path at all (`charts/hermes/templates/servicemonitor.yaml` belongs to a chart ArgoCD never deploys), so nothing is being blocked from scraping. A real corollary was missed instead: `deploy/observability/base/exporters/nats-exporter.yaml:32` scrapes `nats.hermes.svc:8222` cross-namespace, and `allow-nats.yaml:32-36` permits 8222 only from the same namespace. Same for the Postgres and Redis exporters. |
| **16** | `ci.yml` does not hold `id-token: write` and does not push (`ci.yml:174` is `push: false`); that description fits `cd.yml` and `loadtest.yml`. The surface has since **grown**: `sdk-drift.yml` (7 actions), `ci-web.yml` (6) and `loadtest.yml` (4) all landed after the review, none SHA-pinned. `loadtest.yml:29-30,43` holds `id-token: write` and assumes AWS credentials, making it a second OIDC entry point — which also enlarges finding 15. |
| **22** | The mechanism is wrong and the reality is worse. `values.schema.json` has **no** `additionalProperties: false` anywhere, so `services:`, `networkPolicies:` and friends are **silently ignored** rather than rejected — the install succeeds and quietly runs on chart defaults. Seven further wrong keys were found beyond the four filed (`cleanup:`, `migration.enabled`, `adminPortal.replicaCount`, `externalCentrifugo.apiKey`, two `existingSecretKey` values, top-level `image:`), and the one genuinely required value, `global.domain`, is never mentioned. |
| **24** | Over-listed: `cli.md:44-45` and `glossary.md:32` are prose that describes the model correctly and names no JSON fields — not stale. A worse instance was missed: `data-model.md:49-50` still lists the five dropped columns as live DB columns. Coordinates in this finding have drifted ~8 lines. |
| **31** | One of fourteen sub-items is fixed (`TestCreateGroup`, at `68308ad`). The hardcoded account ID appears in **six** non-doc files, not five. |
| **40** | The isolation-model debt finding 42 claimed to discharge is genuinely discharged — verified at `glossary.md:6`, `architecture.md:137`, `integration-guide.md:84`. The four coverage gaps remain. |
| **41** | Sub-claim 2 is fixed for staging only: `overlays/staging/patches/centrifugo-env.yaml:26-34` now sets `CENTRIFUGO_REDIS_PASSWORD` and TLS, added in `65cdb5c` — which **predates the review**. The production patch still stops at the address while its claim sets `transitEncryption: true`. Staging is a ready-made template. |
| **44** | Premise refuted entirely — see the amendment on the finding itself. |

### Two consolidations

**Findings 4 and 27 are the same defect.** `internal/store/postgres/auth.go:94` —
`ON CONFLICT (id) DO UPDATE SET secret = $1` — is both the silent-overwrite path in 4 and the
reason `architecture.md:152-154`'s zero-downtime rotation claim in 27 is false. It is called
unconditionally at startup by admin, inbox and user, each passing `cfg.JWTSecret`, which
defaults to the literal `"hermes-jwt-secret"`. The function's own doc comment at `:89` claims
it "inserts … if it doesn't already exist", which its body contradicts. One trivial fix
closes both findings.

**Finding 9's fix requires inverting two currently-passing tests.**
`internal/delivery/worker_test.go:94-114` and `:116-127` assert the ack-on-failure behaviour
as *correct* — "handleMessage should return nil on provider error". The suite is actively
defending the bug, which is the single most important thing to know before touching it. Note
also that the DLQ machinery is real and wired at the messaging layer
(`internal/messaging/nats.go:227-261`, exercised by `dispatch`); it is dead **only** on the
delivery path, because nothing `worker.go` does can produce a non-nil error. Consequently
`docs/observability/runbooks/dead-letter-queue.md` uses `dlq.delivery.email` as its worked
example for a scenario that cannot occur.

### Smaller additions, folded into their parent findings

- **17** — a third committed placeholder at `deploy/centrifugo/config.json:6`, the Docker
  Compose local-dev config.
- **21** — `secretsmanager:DescribeSecret` is missing from the ESO policy
  (`infra/terraform/modules/eks/main.tf:177` is a bare `GetSecretValue` string, not a list);
  the ACME contact coordinate is `bootstrap-cluster.sh:50`, not `:49`.
- **39** — the 429 response carries no `Retry-After` and is plain text rather than the JSON
  error envelope used everywhere else (`internal/middleware/ratelimit.go:72`). Worse, the two
  API-key services apply `RateLimit` *outside* `APIKeyMiddleware`, so buckets are keyed on the
  raw unvalidated `Authorization` header — every garbage token gets its own bucket, bounded
  only by the 30-minute eviction sweep. Fixing this interacts with finding 3's enforcement work.
- **11 / 31** — `deployment-guide.md:476` deletes a Job named `hermes-migration`; the Job is
  `hermes-migrate`, so the documented recovery step silently no-ops and the subsequent apply
  fails identically.
- **35** — the health check has **two** independent blockers, not one: no ServiceAccount or
  Role grants it `rollout status` permission, *and* the AnalysisRun pod lands in `hermes`
  under `default-deny-all` with no matching allow rule, so it cannot reach the API server
  either. Production's stage has no `verification` block at all, so nothing verifies a
  production promotion.
- **19 / 36** — both force a NATS StatefulSet rolling restart. Do them in one roll.
- **45** — the drift gate's path filter is a hand-maintained package list. It is adequate
  today (verified: `cmd/openapi/main.go:15-18` imports exactly the covered packages, and no
  spec-visible struct references an uncovered one), but it will silently stop covering the
  spec surface if a handler struct ever embeds a type from outside the list.

### Still owed, unchanged

The SDK major version bump ADR 0003 committed to — all four packages remain `0.1.0`. And
ADR 0002's promised follow-up ADR for the normalized content/contact model: `docs/adr/`
still contains only 0001, 0002 and 0003.

---

## Resolved 2026-07-29 — step 2 of the revised order

The trivial, self-contained, no-decision batch. Ten findings, all landed and verified.

**4 and 27 — the shared defect.** `EnsureHermesSigningKey` now uses `ON CONFLICT (id) DO
NOTHING`, reads the stored secret back, and logs a warning when the caller's secret disagrees
— neither secret nor any prefix of one is logged, since that the two differ is the whole
signal. The signature is unchanged, because its three callers in `cmd/` and the interfaces at
`internal/store/interfaces.go:118` and `internal/admin/server.go:72` were out of scope.

The semantics were a decision, not a mechanical fix: plain `DO NOTHING` stops the clobber but
makes `HERMES_JWT_SECRET` silently inert on an existing database, which is a second surprise.
Warn-on-mismatch was chosen over fail-fast so a service booting with a stale variable degrades
loudly rather than refusing to start mid-rollout.

`docs/architecture.md` now says rotation is an explicit database operation coordinated with
Centrifugo's single `token_hmac_secret_key`, and no longer implies a config change performs
one. The multi-key claim is kept — it is true for externally registered issuers.

**Verified by execution, and this one needed it.** The only coverage was
`internal/store/postgres/jwt_keys_test.go`, which asserted the bug as correct ("Second call
updates secret"). That assertion was inverted, not deleted, and the tests moved to
`auth_test.go` beside the implementation. `make verify` does **not** run them — they are
`//go:build integration` — so they were run against a real Postgres: red first
(`stored secret = "secret-2", want "secret-1"`), then green after the fix.

**20 — both JWT gaps.** Per-key `Algorithm` is now enforced via `jwt.WithValidMethods`, so an
HS512-registered key rejects an HS256 token signed with the same secret. An empty algorithm is
treated as HS256 rather than falling back to "any HMAC", which would have been dead code
preserving the exact hole: `migrations/000009_create_jwt_signing_keys.up.sql:4` is
`NOT NULL DEFAULT 'HS256'`, so no writer produces one — but `NOT NULL` still permits `''`, so
it is handled rather than assumed away. The missing-claims guard now uses `claimToString`'s
success return instead of raw map presence, which also closes a same-class hole the finding
did not mention: a present but **empty-string** claim passed too, not just a non-string one.

**13 — SMTP.** `mail.NoTLS` → `mail.TLSOpportunistic`. Opportunistic, not mandatory: STARTTLS
is attempted only when advertised, so the MailHog local default is unaffected while a real
relay now gets an encrypted connection. `go-mail` exposes `Client.TLSPolicy()`, so this is
asserted rather than assumed.

**17 — committed placeholders.** Both `deploy/k8s/base/infra/centrifugo.yaml` and the third
instance found during triage, `deploy/centrifugo/config.json`, now carry empty strings instead
of `CHANGE-ME-…` constants. Empty fails closed — Centrifugo cannot verify a token without a
secret — where the placeholder failed *open*, coming up fully working with a secret published
in a public repository. All three overlays replace this config wholesale, so nothing regresses.

> **`deploy/centrifugo/config.json` appears to be orphaned.** The root `docker-compose.yml`
> defines no Centrifugo service, and the only local Centrifugo runs under Tilt/k3d from
> `deploy/k8s/overlays/local/centrifugo-config.json`. Nothing in this repository consumes it.
> Its placeholder is fixed, but it is a candidate for deletion — flagged rather than deleted,
> since "nothing here consumes it" is not proof nothing does.

**34 — ArgoCD vs the HPAs.** `ignoreDifferences` on `/spec/replicas` for `apps/Deployment`
added to the production Application only; staging includes no `hpa/` and has nothing to
contend. Chosen over deleting `replicas` from the patch so the committed value still seeds a
fresh deployment and still governs the five services with no HPA.

**49 — `sendgrid`.** Dropped from the `values.schema.json` enum. Not implemented, deliberately:
the chart's configmap branches only on `smtp`/`ses` and `internal/email/email.go` rejects
anything else, so the enum was converting a config error into a runtime crash loop.

**26, 29, 31 — documentation.** Counts corrected (9 services, 10 ECR repos and images, the
tenth being `hermes-migrate`). `data-model.md` gained `template_channel_content` and
`user_contact_points` with an updated entity diagram, lost the dropped `users.email`/`phone`
and the five template content columns, and now records that `verified` is inert, that
retention does not run on the DynamoDB path, and that soft-deleted notifications are never
hard-deleted. Eleven of finding 31's items fixed.

### One item in finding 31 was wrong and is withdrawn

**31.2 — `self-hosting/upgrading.md:49` is correct as written and has been left alone.** The
triage recorded "Database migrations cannot be rolled back automatically" as stale because 17
`.down.sql` files exist. That inference does not hold: the files existing does not mean
anything runs them. `cmd/migrate/main.go` exposes only `-database-url` and
`-migrations-path`, and `internal/database/database.go:43` calls `m.Up()` — there is no
direction flag, no step count, no `down`. Implementing the item as filed would have replaced a
true statement with a false one.

31.3 follows from the same fact, so the rollback recipe was rewritten to say rollback is
**manual** rather than repaired with different flags. The two independent defects in it are
real and were fixed: `/migrate` does not exist in that image (`ENTRYPOINT` is `/service`) and
the Job is `hermes-migrate`, not `hermes-migration`, so the documented delete silently
no-opped.

### Deferred, needing a maintainer decision

- **31.12 — repository identity.** Three identities coexist and nothing establishes which is
  intended: `go.mod:1` is `github.com/hermes-notifications/hermes`,
  `charts/hermes/values.yaml:5` publishes to `ghcr.io/hermesnotifications`, and the ArgoCD
  `repoURL`s point at `git@github.com:darylrobbins/hermes.git`. Normalising to any one of them
  is a decision, not an edit, so the docs were left untouched.
- **31.13 — `docs/adr/0001:245`** refers to "keys per namespace" and no namespace concept
  exists in the code. Amending a past ADR is governed by the amend-versus-supersede rule, so
  it is left for a deliberate decision.

### Found while fixing, not previously filed

`docs/deployment-guide.md` told operators to tail logs with
`-l app.kubernetes.io/part-of=hermes`. Per finding 47 that label never reaches a pod, so the
command matches nothing and returns silently. Corrected to select by component, with a pointer
to 47.

---

## Resolved 2026-07-29 — step 3, the NetworkPolicy cluster

Findings **47, 8 and 10**, fixed as one change because fixing any of them alone is either
useless or dangerous: 8 and 10 patch allow rules in a policy set that selected no pods, and
47 alone would have flipped the namespace from *policies inert* to *policies enforced and
wrong*.

**47.** `deploy/k8s/base/kustomization.yaml` now sets `includeTemplates: true` on the `labels:`
transformer, so `app.kubernetes.io/part-of` reaches `spec.template.metadata.labels`. Verified
by rendering both overlays: all 13 pod-producing workloads now carry it, where previously none
did. `includeSelectors` stays `false` deliberately — turning it on would write the label into
`Deployment.spec.selector`, which is immutable on an existing Deployment.

**8.** `hermes-send` added to the `allow-ingress-to-api` selector *and* port 8088 to its port
list. Two independent omissions, either of which alone blocked `/v1/send`.

**10.** Egress to the `observability` namespace on 4317/4318 added, so OTLP can actually
leave the pods. Plus the corollary the finding missed: a new `allow-observability-scrape`
policy permits the observability namespace to reach the NATS, Postgres and Redis exporter
ports, which `allow-nats.yaml` had restricted to same-namespace via `podSelector: {}`.

**Found while fixing, and only visible because 47 was fixed.** `allow-nats-client` permits
6222 *ingress* between NATS pods and nothing permitted the matching *egress*. While the label
never reached a pod this was invisible; with it fixed, a 3-replica JetStream cluster would
have failed to form under default-deny. Added as `allow-nats-cluster-egress` in both overlays.

### A regression gate, because this defect class is invisible by construction

`kustomize build` succeeds, `kubectl apply` succeeds, and the API server accepts a
NetworkPolicy matching nothing. An inert policy is indistinguishable from a working one until
enforcement is switched on. So `scripts/check_networkpolicy_selectors.py` now fails
`make verify-manifests` if any policy's `podSelector` matches no workload, and it names
`includeTemplates` as the likely cause.

Proven to bite: reverting the one-line fix makes `make verify-manifests` exit non-zero with
`FAIL: 3 of 9 NetworkPolicies select no pods`. The script has 11 unit tests, also run by
`verify-manifests`.

> The count differs between the mutation run (3) and the original triage (4) because the
> fourth, `allow-nats-client`, keys its *source* selector on `part-of` while its `podSelector`
> keys on `name: nats`. It selects a pod either way — but its ingress rule matched no source
> until the label was fixed. A `podSelector` check cannot see that, so the gate is necessary
> but not sufficient. Peer selectors inside rules remain unchecked.

### Still open in this cluster

- **Enforcement is still off.** `infra/terraform/modules/eks/main.tf` declares the `vpc-cni`
  addon with no `configuration_values`, so `enableNetworkPolicy` is unset and none of these
  policies are enforced. That is now a deliberate choice rather than an oversight: the policy
  set is correct as declared, and turning enforcement on is a separate, reviewable change.
- **Finding 8's capacity half is untouched.** `hermes-send` is still absent from the replicas
  patch, resources patch, HPA set, PDB set, anti-affinity patch and the Kargo health check, so
  it stays at `replicas: 1` in production. Those are capacity decisions, not correctness fixes.
- **Finding 35** is unaddressed: the Kargo health check has no ServiceAccount or RBAC and its
  AnalysisRun pod cannot reach the API server under default-deny. Two independent blockers.
- **Email egress is unresolved and deliberately not guessed at.** No `HERMES_EMAIL_PROVIDER`
  is set in either overlay, so whether the email worker needs SMTP egress (587/465/25) or is
  covered by the existing 443 rule depends on findings 23 and 41. No rule was added on
  speculation.

---

## Resolved 2026-07-29 — step 4 (partial), the small security items

Findings **15, 16** and most of **21**. Findings **1, 3 and 14** are deliberately not
attempted here — see below.

**16 — all 38 action references pinned to commit SHAs** across all six workflows, each with
the tag in a trailing comment (`uses: actions/checkout@d23441a… # v6`). Every tag was
resolved through the GitHub API rather than copied from anywhere, dereferencing annotated
tags to their commit. That also settles the note in the `68308ad` commit message recording
`actions/setup-java@v5` and `actions/setup-dotnet@v5` as unconfirmed: both exist and resolve.

Pinning is only half of it — a pin that nobody bumps is a stale dependency wearing a
security control's clothes. `.github/dependabot.yml` already carries a weekly
`github-actions` entry, and Dependabot understands the SHA-plus-comment form and updates
both parts. That entry is now commented to say so, because deleting it would silently freeze
every action at whatever commit was current when it was pinned.

Image signing and provenance, the finding's second half, is **not** done. It needs an
admission-time verifier that does not exist in `deploy/` and is properly its own piece of work.

**15 — OIDC trust narrowed** from `repo:<org>/<repo>:*`, which trusts every ref in the
repository, to exactly `refs/heads/main` and `refs/tags/v*` — the two that `cd.yml` actually
uses. This deliberately also narrows `loadtest.yml`, which is `workflow_dispatch` with
`id-token: write` and could previously assume the role from any branch; it now works only when
dispatched from main.

**21 — five of eight sub-items fixed.**

- Raw API keys are no longer map keys. The bucket key is now a SHA-256 digest, applied
  *inside* `RateLimit` rather than at each call site so no caller can forget it. The raw
  bearer token — not even stripped of its `Bearer ` prefix — was previously a live map key
  retained for up to 30 minutes by the eviction sweep, so any heap profile carried working
  credentials. Note this does **not** bound the map: an unauthenticated caller sending
  garbage still mints a bucket per distinct value, because `RateLimit` is applied outside the
  auth middleware. That is finding 39 and needs the ordering changed, not a different hash.
- `automountServiceAccountToken: false` on the `hermes` ServiceAccount. No service talks to
  the Kubernetes API.
- `force_delete` on ECR is now `var.environment != "production"`. It previously let
  `terraform destroy` remove a repository full of images, quietly undoing the point of the
  `IMMUTABLE` tag policy on the line above it.
- The `recoveryWindowDays` XRD default moved from `0` to `7`. A default is what a claim gets
  when its author has not thought about it, and `0` — immediate, unrecoverable deletion — is
  the least recoverable value available. Staging still sets `0` explicitly, which is fine
  because it is a choice rather than an accident.
- `secretsmanager:DescribeSecret` added to the ESO policy, still scoped to `secret:hermes/*`.
- `nodes/proxy` dropped from the Datadog ClusterRole; it permits arbitrary requests through
  every node's kubelet API, far beyond what metrics collection needs.

Not done: **VPC flow logs**, which need a destination and retention decision rather than a
line of code, and the **ACME contact address**, which is a real address belonging to the
repository owner and is theirs to change.

### Not attempted, and why

- **1 (NATS unauthenticated and unencrypted)** is the largest item in the review and is
  genuinely entangled: it needs a config surface, secret plumbing through ExternalSecrets, a
  NATS accounts/users configuration, and certificate issuance — and it overlaps findings 14
  and 19. It changes an auth model, so per `CLAUDE.md` it warrants an ADR before code.
- **3 (permission enforcement)** is not mechanical either. `RequirePermission` has no call
  sites *because* its `func(http.Handler) http.Handler` shape does not fit Huma's routing;
  enforcing per-operation permissions means choosing a different integration point. That
  choice should be made deliberately.
- **14 (datastore TLS)** is small as a config surface and large as a real capability — a CA
  bundle and client certificates are a different piece of work from adding a field. It shares
  plumbing with finding 1 and should land with it.

### Unverified

The Terraform edits (findings 15, 21) are **not** validated: `terraform` is not installed in
this environment, so `terraform fmt` and `terraform validate` could not be run. The HCL is
syntactically conventional and `make verify` covers everything else, but the Terraform itself
has been read, not executed.

---

## 2026-07-29 — ADR 0005 and phase 1 of the transport-security work

Findings **1, 14 and 19** were repeatedly deferred as "large" without anyone deciding the
shape of the work, which is the state in which items stay deferred indefinitely.
[ADR 0005](../adr/0005-transport-security-for-infrastructure-connections.md) now decides it:
NKey authentication plus TLS for NATS, an explicit TLS configuration surface for the
datastores, a private cert-manager issuer for in-cluster certificates, and three separately
shippable phases.

Two facts found while writing it change the work, and both are worth pulling out:

- **The Redis half is nearly free.** `compositions/aws/cache.yaml` already sets
  `atRestEncryptionEnabled`, wires an `authTokenSecretRef` with `ROTATE`, and maps
  `spec.transitEncryption` through; production already claims `transitEncryption: true`. The
  infrastructure is provisioned for authenticated TLS and only the client is not using it.
- **cert-manager is installed but cannot issue what finding 1 needs.**
  `bootstrap-cluster.sh` creates a *Let's Encrypt* ClusterIssuer — a public ACME issuer that
  can serve the ingress domain and cannot issue for `nats.hermes.svc`. The tool is present;
  a private issuer is not. Any estimate for finding 1 that assumed cert-manager was ready
  was wrong.

**Phase 1 has landed.** `internal/config` gained an `Environment` field (`HERMES_ENV`) and a
`Validate()` that, outside development, rejects a database URL without a secure `sslmode`,
a non-`rediss://` Redis URL, a non-`tls://` NATS URL, and any of the three built-in
placeholder secrets. All nine services now call `MustLoad()`, so a service that cannot reach
its datastores securely refuses to start instead of connecting in the clear.

`Validate` inspects the connection strings themselves rather than parallel "TLS enabled"
settings, because two settings that can disagree are worse than one that cannot. It reports
every problem at once — nine restarts each revealing one more misconfiguration is a bad way
to learn what is wrong. `allow` and `prefer` are excluded from the accepted `sslmode` values
deliberately: both silently fall back to plaintext, which is the exact failure this closes.

**This also closes finding 4's second half.** The insecure defaults now have the environment
gate the finding asked for. Their first half — the signing-key overwrite — was fixed earlier
today.

> **The gate was nearly inert on arrival.** Nothing set `HERMES_ENV`, so every deployment
> would have defaulted to `development` and skipped every check — a control that exists and
> does not run, which is precisely finding 47's pattern. `HERMES_ENV` is now set in both
> overlay `configMapGenerator`s and its presence verified in the rendered output. Worth
> recording as a near miss: a validation nobody triggers is indistinguishable from one that
> passes.

Phases 2 (NATS TLS) and 3 (NATS accounts and per-service subject permissions) remain open,
and finding 19's `securityContext` should land with phase 2 because both change the NATS
StatefulSet and both force the same rolling restart.

**Sequencing constraint, recorded in the ADR:** phase 1's Postgres half cannot be completed
until **finding 12** is fixed. `HERMES_DATABASE_URL` is assembled from Crossplane connection
details by the `HermesSecretsBundle` composition, which is a no-op — so whoever fixes 12
chooses the `sslmode`. The validation now in place will reject the result if they choose
wrongly, which is the intended order of events.

---

## 2026-07-29 — finding 12 partially resolved, finding 28 corrected

Finding 12 turned out to contain a design decision rather than a missing line. The
composition can derive four of the eight properties the ExternalSecrets read; the other four
— `jwt_secret`, `api_key_hmac_secret`, `centrifugo_token_secret`, `centrifugo_api_key` — are
application secrets Crossplane cannot invent. A Secrets Manager secret has a single
`secretString`, so whoever writes it owns all of it, and one secret containing both kinds
means Crossplane reverts whatever an operator seeds.

**Decision (2026-07-29): split by ownership.**

| Secret | Owner | Contents |
|---|---|---|
| `hermes/<env>/connection` | Crossplane, reconciled | `database_url`, `redis_url`, `centrifugo_redis_address`, `centrifugo_redis_password` |
| `hermes/<env>/app` | Operator, seeded once | `jwt_secret`, `api_key_hmac_secret`, `centrifugo_token_secret`, `centrifugo_api_key` |

Crossplane creates both containers and writes a version only for `/connection`, so an
operator-seeded value in `/app` is permanent. Both overlays' ExternalSecrets now read from
the correct one, and `docs/deployment-guide.md` has the seeding procedure.

**This also corrects finding 28**, which the triage had parked as needing a maintainer
decision. The split makes the answer plain: `database_url` lives in the Crossplane-owned
secret and `compositions/aws/database.yaml` sets `autoGeneratePassword: true`, so the
documented rotation — `aws rds modify-db-cluster` followed by a hand-written
`put-secret-value` — could never have held. The guide no longer instructs it. The replacement
is explicitly marked as unverified shape rather than tested recipe.

### What is NOT done, and why it was not guessed at

**The `/connection` secret is still not populated.** Assembling it means reading the
connection Secrets that the Aurora and ElastiCache compositions write, which requires
`function-extra-resources`. That function is installed in `functions.yaml` but **is used by no
composition in this repository** — there is no working pattern here to copy, and no cluster
available to develop one against.

Writing it blind would produce a composition that reconciles successfully and delivers
nothing, which is the exact defect finding 12 describes. So the gap is now marked in the
composition itself, and the deployment guide documents seeding `/connection` by hand as the
interim, including the two constraints `config.Validate` enforces at startup (`sslmode=require`
or stricter, and `rediss://`).

Net effect: four of the eight properties are now workable end-to-end via manual seeding, up
from zero, and the ownership question that blocked the other four is settled rather than open.

**Unverified:** none of this has been applied. `make verify` covers YAML validity and nothing
else — no cluster, and `terraform` is not installed in this environment either.

---

## Resolved 2026-07-29 — finding 3, the unenforced permission system

The finding said `auth.RequirePermission` was defined, unit-tested, and had **zero**
production call sites. The triage confirmed it and added the reason: its signature was
`func(http.Handler) http.Handler`, and every service routes through Huma, whose handlers
are `func(ctx, input)` and never see an `http.Handler`. **The middleware could not be
applied to any operation, so its tests exercised a function no route could use.** That is
worth stating plainly, because a fully-tested security control that cannot be wired up
looks from a coverage report exactly like one that works.

Replaced with `auth.CheckPermission(ctx, perm) error`, which takes the context a Huma
handler actually has, plus a thin `requirePermission` in each service mapping it onto
Huma's error types. Now enforced on:

| Route group | Permission | Was |
|---|---|---|
| `/v1/send` | `notifications:send` | **unenforced** |
| `/v1/templates` (all 4 ops) | `templates:manage` | **unenforced** |
| `/v1/organizations` (both ops) | `organizations:manage` | **unenforced** |
| `/v1/apikeys` (all 3 ops) | `apikeys:manage` | enforced, but fail-open |

The send case is the one that mattered: `/v1/send` is the platform's primary write path,
and a key issued narrowly for template management could forge notifications to any user in
any organization. The test that proves it now was watched failing with **`202 Accepted`** —
the notification was genuinely sent.

**Fail-open is gone.** The three existing checks read
`if key != nil && !auth.HasPermission(...)`, which *passes* when the key is nil. It was
unreachable in production because `APIKeyMiddleware` returns 401 first, so the triage
correctly downgraded it from the finding's claim — but it was fail-open by construction, and
one differently-mounted route away from being live. `CheckPermission` now fails closed
unconditionally.

That was only possible by fixing what made the nil-check necessary: `SetSkipAuth(true)`
removed the auth middleware entirely, leaving no key in context, so handlers had to tolerate
nil for tests to pass. Skip-auth now injects a synthetic key holding every permission, so
tests run the same code path as production. **A security control weakened for the
convenience of tests protects nothing.**

### Gaps left open, deliberately

**Four route groups still enforce nothing**, because no permission constant covers them and
inventing one is an API decision, not a fix: `/v1/subscriptions` and
`/v1/subscriptions/categories`, `/v1/auth` (token exchange — note this issues a JWT for an
arbitrary user, so it is the most consequential of the four), `/v1/notifications`, and
`/v1/users`. `AllPermissions` has four entries and the service has eight route groups.
Adding constants would also change what existing keys need, so it wants a deliberate
decision about the permission model rather than an incremental patch.

**Permission checks run after input validation.** Huma validates the request body before
invoking the handler, so a malformed request from an unauthorized caller returns 422 rather
than 403. No meaningful information leaks — 422 is returned identically whether or not the
caller is authorized — but it is worth knowing before writing a test that expects 403 and
sees 422, which is exactly what happened while writing these.

**`internal/auth/middleware.go` had no tests at all** before this change, despite being the
authentication boundary. It now has them, including that health endpoints bypass auth and
carry no key — which is what makes `CheckPermission`'s fail-closed behaviour safe in
production, since an unauthenticated request never reaches a handler.

---

## Resolved 2026-07-29 — finding 3 completed, finding 35, and 31.12 settled

**Finding 3's remaining gap is closed.** The four ungated route groups are folded onto the
existing constants rather than given new ones: subscriptions and categories under
`templates:manage`; users, notifications and token exchange under `organizations:manage`.
Folding was chosen over new constants because new ones would mean every existing key lacks
them, forcing either a widened `DefaultPermissions` or a reissue. The cost is that the
mapping is looser than purpose-built permissions would be — `auth:issue` is not really
organization management — and that is the trade recorded here rather than hidden.

All eight route groups now enforce a permission: **22 handler-level checks**, up from three.

The `/v1/auth` case is the one that mattered. Its test was watched failing with **200 OK** —
a key holding only `notifications:send` minted a JWT for an arbitrary user, which is
impersonation of anyone in the organization.

**Finding 35** had two independent blockers and both are fixed. A ServiceAccount, Role and
RoleBinding scoped to reading Deployments, StatefulSets and Pods in `hermes` only — the
AnalysisRun previously ran as `default`, which holds nothing, so every `kubectl` call
returned 403. And an egress NetworkPolicy, because the pod lands under `default-deny-all`
and could not reach the API server even with RBAC. The pod now carries a label the policy
selects on; without one the policy would match nothing, which per finding 47 looks exactly
like a policy that works. `hermes-send` was also missing from the rollout loop, so the
platform's primary write path was never verified after a promotion.

The image was `bitnami/kubectl:latest`. It is now pinned by digest — an unpinned
verification gate is one where a new image silently changes what "healthy" means between two
promotions of identical code.

> **Caught while writing this:** I first pinned to `bitnami/kubectl:1.31.0`, a tag I had not
> checked. `docker manifest inspect` shows it does not exist. The digest now in the file was
> resolved from `:latest` and verified. Worth recording because an invented-but-plausible
> tag would have failed only at promotion time, in the gate meant to catch failures.

**Production still has no `verification` block, deliberately.** Adding one makes a failing
health check block production promotions, and this check has never executed — it could not
have, given the two blockers above. Wire it to production only after it has passed in
staging at least once. The review's own advice was to fix the check first and connect it
after; that ordering is being followed rather than restated.

**31.12 is settled:** the repository is `github.com/darylrobbins/hermes`, and the docs' one
plainly wrong GitHub URL is corrected. The other two identities are **not** changed: `go.mod`
declares `github.com/hermes-notifications/hermes` and the chart publishes to
`ghcr.io/hermesnotifications`. Changing the module path would touch every import, and the
registry namespace is the chart's to decide — so both are flagged in
`self-hosting/configuration.md` where a reader would trip over them, rather than silently
normalised.

---

## Added and resolved 2026-07-29 — finding 50, found while fixing finding 9

**50. [P0] The dead-letter queue could not capture a malformed payload — the exact case it
exists for.** `hermenats.DeadLetter.Payload` was typed `json.RawMessage`, whose
`MarshalJSON` validates its contents. A payload that was not valid JSON therefore made
`DeadLetter.Marshal` fail, `publishDeadLetter` returned an error, and `processMessage` fell
back to nacking the message — which then retried until `MaxAge` and was lost.

The failure was **silent**: the publish error only incremented a counter, and
`internal/messaging` has no logger. From outside, a message that could not be dead-lettered
was indistinguishable from one being retried normally.

This was invisible before finding 9 because the delivery workers never returned an error at
all, so nothing on that path ever reached `publishDeadLetter` with an unparseable payload.
Fixing finding 9 exposed it immediately — the first E2E test to drive a failing delivery to
its conclusion hit it on the first run.

Fixed by typing `Payload` as `[]byte`, which marshals as base64 and always round-trips,
including for truncated and binary bodies. **This changes the DLQ wire format**: `payload`
is now a base64 string rather than inline JSON, so anything reading the DLQ must decode it.
`docs/observability/runbooks/dead-letter-queue.md` is updated, including its replay
commands, which would otherwise have been wrong.

### Finding 9 resolved

`internal/delivery/worker.go` returned `nil` on every failure path, which the messaging
layer reads as success — so the message was acked and dropped, and the retry, backoff and
dead-letter machinery was unreachable from all three delivery workers. A transient SMTP or
webhook blip permanently lost the notification.

Now: an unparseable message returns a **permanent** error (retrying cannot help, so it is
terminated straight to the DLQ with reason `terminated` rather than burning ten attempts),
and a provider failure returns a **transient** error, so the message is nacked, retried with
backoff and dead-lettered once retries are exhausted.

Provider errors are deliberately *not* classified per-provider: `Provider.Send` returns a
bare error, so there is no way to distinguish a 4xx rejection from a connection refused.
Treating them all as transient is the safe default — a permanent misclassification drops a
deliverable notification on attempt one. Per-provider classification is worth doing and is
its own change.

The `<channel>.failed` event now fires only on the final attempt. Publishing on every
attempt would put up to `maxDeliveries` failure events on the stream for one notification,
which the status rollup and any alert counting them would both misread.

**The two tests that pinned the bug were inverted, not deleted**, with the reason recorded
in the test file. `worker_test.go` had required `handleMessage` to return nil on both
provider and unmarshal errors — so the suite was defending the defect, and any correct fix
would have "broken" a passing test.

**A new E2E test drives a failing delivery to the DLQ** (`tests/e2e/dlq_test.go`). Nothing
previously did, which is why finding 9 and finding 50 both survived. It uses the permanent
path rather than exhausting `maxDeliveries`, because that constant is package-private to
`internal/messaging` and draining ten attempts with backoff would be slow and flaky.

Verified by execution against real infrastructure: Postgres, NATS, Redis, Mailpit and
DynamoDB-local, with the full `go test ./... -tags=integration` suite green. The DLQ test
went from timing out at 20s to passing in 0.03s.

---

## Resolved 2026-07-29 — findings 25, 30, 32 and 40's coverage gaps

**25 — the dual store is documented.** `architecture.md` gained a *The dual store* section
and `HERMES_DYNAMO_ENDPOINT`/`HERMES_DYNAMO_REGION` are in `configuration.md`, alongside
`HERMES_ENV`, which ADR 0005 phase 1 added and nothing documented. The three consequences
that matter are stated rather than implied: Postgres stays **required** (the Dynamo store
delegates to it), `cmd/cleanup` does not run on that path so retention is TTL-only and
unverified, and no backup mechanism for the table exists in this repository.

**30 — the shared-packages table** in `services.md` now lists `internal/store/dynamo` and
`internal/provider`, and the glossary has a **Provider** entry drawing the channel/provider
distinction — including that delivery subjects remain per-channel, so which provider runs is
decided inside the worker rather than by routing.

**40's cursor and backup gaps are closed.** `integration-guide.md` now warns integrators that
cursors encode backend-specific state and are rejected across a backend switch, with the
instruction to discard and re-request rather than persist them — ADR 0001 recorded this and
no integrator-facing document had ever mentioned it. `self-hosting/production.md` gained a
Backup and Restore section, which it previously lacked entirely.

That section is deliberately labelled as untested: **a backup you have not restored from is a
hypothesis**, and none of it has been rehearsed. It does name two non-obvious restore hazards
— `jwt_signing_keys` must come back with the dump or every issued JWT silently dies, and
`schema_migrations` must match the deployed image because `cmd/migrate` only goes forward.
It also warns that the chart's `networkPolicy.enabled: true` is not isolation, since every
rule has an empty peer list (finding 41).

### Finding 32 fixed, and now enforced rather than merely described

The channel-resolution order was wrong in four places: `architecture.md`, `glossary.md`,
`CLAUDE.md`, and the doc comment on `ResolveChannels` itself. All four described a three-way
precedence — explicit → user preference → category default — that the data model cannot
express, because `user_subscriptions` has `opted_in` and no channel column.

What the code does: a user preference is a **boolean gate over an already-resolved set**. A
`required` category skips the preference check entirely. Explicit channels **replace** the
category default wholesale rather than merging with it. Two narrowing passes run afterwards
and are not part of resolution.

**The important part is that this is now pinned.** `ResolveChannels` had zero test coverage —
its only reference outside its own definition was the single call site in `dispatch.go` — so
nothing would have failed if someone had "fixed" the code to match the wrong documentation
instead of the other way round. `internal/dispatch/channel_resolution_test.go` now covers
every branch, including the two with the most user-visible consequence and no prior coverage:
a `required` category ignoring an opt-out, and `default_state = "off"`.

Mutation-checked: disabling the `required` branch fails
`TestResolveChannels_RequiredCategoryIgnoresOptOut` with `got [], want [email]`. Correcting
prose is cheap and rots; a test that fails is what keeps it true.

---

## Suggested remediation order

> **Superseded 2026-07-29 — see "Revised remediation order" below.** The original order is
> kept for the record. Its step 1 rests on findings 8 and 10 being "broken in both deployed
> environments right now", which the triage showed is almost certainly false: nothing is
> deployed, and the policies are not enforced anyway (finding 47).

1. **Immediately** — findings 8 and 10, which are broken in both deployed environments right
   now, then the security workstream 1, 3, 4, 5, 6, 7, then 9, 11, 12.
2. **Next** — the P1 security hardening batch (13–21), then the three documentation
   findings that break users (22, 23, 24) and the dual-store gap (25).
3. ~~**The rename (42) is schedule-driven, not priority-driven.**~~ **Done** — landed at
   `a5f0874`, ahead of both ADR 0001 Phase 2 and ADR 0002's subject freeze. What remains is
   its residue: **43** is now resolved too, leaving only the major version bump as a release
   decision; **44** (audit dashboards and alert rules for the old metric label) belongs in
   step 4; **45** (spec-to-SDK CI gate and toolchain pinning) belongs in step 2, because it
   is what allowed 43 and will allow the next one. **46** (no JS/TS test runner) belongs
   beside 45 for the same reason — both are detection gaps, and a detection gap outranks
   most of the individual defects it hides.
4. **Then** — accuracy drift (26–32) and operational gaps (33–41, 44), including the new
   documentation sections in 40. Note that several of these findings cite line numbers in
   files the rename rewrote, so expect drift when working through them; the claims were
   re-verified, the coordinates were not.

## Revised remediation order (2026-07-29)

Reordered around two facts the triage established: **nothing is deployed yet**, and the
NetworkPolicy set does not select its own pods and is probably not enforced (finding 47).
Coordinates throughout the findings were re-anchored during the triage, so the caveat in
step 4 above no longer applies.

1. **While nothing is deployed — take the free wins that stop being free.** 38 (VPC CIDRs:
   one line per tfvars now, an environment rebuild later), 5's KMS envelope encryption
   (irreversible once a cluster exists), then 6, 7, 12, which carry no reconciliation risk
   while there is nothing to reconcile. This step has a deadline that the others do not.
2. **The trivial, self-contained, no-decision batch.** 4+27 (one `ON CONFLICT` line closes
   both), 13 (SMTP silently disabling TLS), 20 (both JWT gaps), 17 (committed placeholders),
   26/29/31 (editorial), 34 (`ignoreDifferences`), 49 (drop or implement `sendgrid`). Each is
   independently verified, repo-only, and needs no decision.
3. **The NetworkPolicy cluster, as one change.** 47 first — the label bug — then 8, 10, and
   35's egress blocker, then decide explicitly whether to enable `enableNetworkPolicy` on the
   VPC CNI addon. Doing 8 or 10 alone is wasted work; doing 47 alone flips the namespace from
   inert to enforced-and-wrong. Validate with `kubectl kustomize` against both overlays.
4. **The remaining security workstream.** 1 (NATS auth — the large one, entangled with 14 and
   19), 3 (permission enforcement — note the Huma middleware shape is why `RequirePermission`
   has no call sites), 14, 15, 16 (pin the actions, including the three workflows added since
   the review), 21.
5. **Delivery correctness.** 9, which needs explicit sign-off that the two tests pinning
   ack-on-failure are wrong before they are inverted. Then 11, 33, 35, 36, 19.
6. **Documentation that breaks users**, now that 22's real failure mode is understood as
   silent no-op rather than validation failure: 22, 23, 24, 48. Then the dual-store
   documentation workstream under 25, which is larger than originally filed now that the
   DynamoDB backend is a supported option, and which should land together with 30 and 40's
   backup gap since they touch the same files.
7. **Everything else** — 28, 32, 37, 39, 40, 41, and 44's single remaining out-of-repo check.

## Follow-ups

- ~~[ADR 0003](../adr/0003-rename-tenant-to-organization.md) is implemented but still
  `Status: Proposed`~~ done at `68308ad` — both the ADR and `docs/adr/README.md:15` now read
  `Accepted`, amended in place with a date per `adr/README.md:56-69`. (This entry sat stale
  in two places after the work had landed; corrected 2026-07-29.)
- ~~Regenerate the four SDKs~~ done 2026-07-28; **the major version bump ADR 0003 commits to
  is still owed** — finding 43.
- ~~Audit out-of-repo Grafana dashboards and alert rules for the `tenant_id` label~~ premise
  refuted 2026-07-29 — the renamed token was never an emitted label and the in-repo dashboards
  and rules never referenced it. **Replaced by:** verify the unversioned production/staging
  collector `values.yaml` (the one carrying the Datadog pipeline fan-out) still deletes
  `organization_id`, or its cardinality guard is a no-op. See the amendment on finding 44.
- **Decision 2026-07-29: the DynamoDB backend is a supported deployment option**, not an
  internal experiment. This enlarges finding 25 into a documentation workstream — see the
  scoping note on that finding. The integrator-facing cursor-incompatibility warning
  (ADR 0001:225-232) is now a defect rather than an internal note.
- **Decision 2026-07-29: neither environment is deployed.** Findings 5, 6, 7, 12 and 38 are
  cheap now and expensive later; see step 1 of the revised remediation order.
- ~~Add the spec-to-SDK drift gate to CI and pin the generation toolchain~~ done — the gate
  landed at `68308ad`, the `packageManager` pin on 2026-07-28 alongside finding 46.
- ~~Add a JS/TS test runner and a first test~~ done 2026-07-28. The `web/admin/app/**/layout.tsx`
  exemption was **kept** deliberately, not dropped — see the resolution note on finding 46.
- **New, from finding 46's fix:** `hermes-react` and `hermes-web` had a `test` script added
  but `hermes-client` and `hermes-server` still have none. Both are generated-type wrappers
  where `tsc` carries most of the weight, but `RealtimeConnection` in `hermes-client`
  (`src/realtime/connection.ts`) is hand-written runtime code — URL scheme rewriting and
  publication-to-event mapping — and is untested. Worth a suite when someone next touches it.
- ADR 0002 committed to a follow-up ADR for the normalized content/contact model; PR #43
  shipped that phase without one. Still owed.

## Note on what went well

The observability documentation is the best-maintained set in the tree and is worth copying
as the model. `observability/README.md:23` and `architecture.md:12,40-41,65,78` correctly
cover the optional SigNoz fan-out from commit `55430f8`, and
`observability/adr/001-lgtm-over-signoz.md:4` was properly amended (dated 2026-06-13) *ahead
of* the code landing rather than silently rewritten — exactly the amend-versus-supersede
discipline `adr/README.md:56-69` prescribes and the rest of the tree is missing.
