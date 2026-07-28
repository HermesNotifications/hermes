# Architecture Review — 2026-07-27

Findings from a full review of the architecture documentation, the code it describes, and
the deployment/infrastructure configuration.

**Reviewed at:** `68f0996` (main)  
**Last updated:** 2026-07-28, after the organization rename landed at `a5f0874` and the SDKs
were regenerated  
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

**Severity is deployment-readiness, not live breakage.** The staging and production
environments exist and are reconciled by ArgoCD, but neither carries real users, real
traffic, or real data, and there are no external SDK consumers — the same premise ADR 0003
relies on to justify a clean break with no compatibility window. So "P0" here means *this
will fail, or is already failing, the moment the environment carries load*, not *customers
are affected right now*. Nothing below is downgraded on that basis: pre-production is the
cheap time to fix all of it, and the absence of traffic is why several of these have gone
unnoticed rather than a reason to defer them.

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

Grafana dashboards, saved queries, and any alert rule that groups by the old label live
outside this repo and still reference `tenant_id`. They do not error; they silently return
no series. Alert rules are the sharp edge — one grouping by `tenant_id` stops firing rather
than starts failing, and the pre-production quiet makes that indistinguishable from healthy.
Audit the LGTM dashboards and alert rules for the old label, and treat this as the worked
example for the next rename that touches a metric label.

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

---

## Suggested remediation order

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

## Follow-ups

- [ADR 0003](../adr/0003-rename-tenant-to-organization.md) — **implemented at `a5f0874`, but
  still `Status: Proposed`** in the ADR and in `docs/adr/README.md:15`. Flip both to
  `Accepted`. Per `adr/README.md:56-69` this is a clarification, so amend in place with a
  date rather than superseding.
- ~~Regenerate the four SDKs~~ done 2026-07-28; **the major version bump ADR 0003 commits to
  is still owed** — finding 43.
- Audit out-of-repo Grafana dashboards and alert rules for the `tenant_id` label — finding 44.
- Add the spec-to-SDK drift gate to CI and pin the generation toolchain — finding 45.
- Add a JS/TS test runner and a first test, then drop the `web/admin/app/**/layout.tsx`
  exemption from `.claude/guardrails.json` — finding 46.
- ADR 0002 committed to a follow-up ADR for the normalized content/contact model; PR #43
  shipped that phase without one. Still owed.

## Note on what went well

The observability documentation is the best-maintained set in the tree and is worth copying
as the model. `observability/README.md:23` and `architecture.md:12,40-41,65,78` correctly
cover the optional SigNoz fan-out from commit `55430f8`, and
`observability/adr/001-lgtm-over-signoz.md:4` was properly amended (dated 2026-06-13) *ahead
of* the code landing rather than silently rewritten — exactly the amend-versus-supersede
discipline `adr/README.md:56-69` prescribes and the rest of the tree is missing.
