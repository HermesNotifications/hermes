# Provider Plugin Model

## Context

Hermes delivers notifications through a fixed set of channels — `email`, `sms`, `inbox` —
each implemented as a separate worker binary (`cmd/worker-email`, `worker-sms`,
`worker-inbox`) that subscribes to its own `delivery.<channel>` NATS subject and delivers
via the `delivery.Provider` interface (`Send(ctx, req) → DeliveryResult`). The seam for
pluggability already exists: per-channel workers, per-channel JetStream work queues, and a
single-method delivery interface. Email even has two providers (SMTP, SES) selected by
static config.

But the model is closed in three ways that block where we want to go:

1. **Channels are hardcoded.** `email`/`sms`/`inbox` appear as `switch ch` blocks in
   `FilterChannelsForTemplate` (`internal/dispatch/channels.go`), the contact-info filter
   and `contentForChannel` (`internal/dispatch/dispatch.go`), and as fixed template
   columns (`EmailSubject`, `SMSBody`, `InboxTitle`, …). Adding a channel means editing
   core in several places.
2. **Provider selection is static, not routed.** SMTP-vs-SES is a config switch, not a
   per-notification decision. There is no failover, no cost-aware routing, no fan-out, and
   no path to per-tenant providers.
3. **Delivery status is one-way.** Events flow only worker → `notification.events` at send
   time. Providers report delivery outcomes asynchronously and out of band (SES → SNS →
   SQS; Twilio webhook), and there is no way for those receipts to flow back in.

This design introduces a **provider plugin model**: a clean channel/provider split, a
data-driven routing policy with failover/fan-out and a central retry ladder, an inbound
delivery-receipt pipeline, and a registration/trust model that supports both **built-in
(first-party)** providers compiled in and enabled by config, and **third-party** providers
run as isolated processes speaking the NATS contract.

> **Terminology.** A **provider** is the pluggable delivery unit (ses, smtp, twilio,
> sendgrid). It matches the existing `delivery.Provider` interface and `ProviderID`/
> `ProviderName` fields. A **channel** is the user-facing medium (email, sms, inbox, push,
> slack) — the preference axis users subscribe to and templates render for. One provider
> serves exactly one channel.

### Goals

- One provider interface, two hosting modes (in-process built-in, out-of-process
  third-party) so a provider can graduate between them without code changes.
- Deploying with built-in providers is trivial and zero-extra-infra; the operator enables
  **0 or more** providers per channel by config.
- De-hardcode channels: adding a channel is registering metadata, not editing switches.
- **Providers stay dumb.** A provider's entire job is "attempt one delivery, return success
  or a classified error." All routing, retry, fallback, and fan-out are the platform's job.
- Data-driven routing with **first|all** semantics at both the channel and provider level,
  **failover**, **cost** awareness, and a direct **per-tenant override** seam.
- A three-tier **retry ladder** (fast provider retries → fallback → long-horizon channel
  retry with exponential backoff), driven centrally and fed by the DLQ.
- An inbound **delivery-receipt** pipeline that reuses the existing Event Writer rollup.
- Mixed trust: third-party providers are isolated by process boundary and scoped NATS
  credentials, never touch the database or the public internet directly, cannot fabricate
  delivery events, and need only a minimal cred surface (consume own subject, publish own
  receipts).

### Non-goals (explicitly deferred)

- Per-tenant providers — deferred to a later phase (phase 8). Day 1 only **reserves the
  resolution-order slot** and the tenant→global config/secret fallback seam; the
  `tenant_channel_override` table, its resolution step, and CRUD land later.
- A full routing expression language (CEL/AND-OR-NOT) — day 1 is config order + simple
  equality-predicate conditions, first match wins.
- WASM / in-binary dynamic loading — rejected as primary (see Alternatives); kept as a
  future option for untrusted-no-deploy compute.
- Circuit-breaking is a **later phase** (not day 1) — early phases rely on the retry
  ladder's failover; health-based liveness skipping arrives with health reports (phase 7) and
  the breaker after that (phase 9). See §7.

### Decisions made during brainstorming

- **Isolation model: bus-native plugins (NATS subject is the contract).** Chosen over
  subprocess gRPC (HashiCorp go-plugin) and in-binary (Go `plugin`/WASM).
- **Trust: mixed** — first-party and third-party authors both supported.
- **Subjects: dispatch selects the provider** and publishes `delivery.<channel>.<provider>`
  (per-provider work queues + DLQ partitions, explicit routing).
- **Content model: fully normalized** — per-channel template content and recipient
  addresses move out of fixed columns into channel-keyed tables.
- **Providers are dumb; routing/retry is central.** No routing or retry logic lives in a
  provider. Failures feed back to a dedicated **Redelivery service** via the DLQ.
- **first|all at two levels.** A policy selects channels with a `channel_mode` (first|all);
  each channel has an ordered provider list with a `provider_mode` (first|all). `first` =
  failover (first success wins), `all` = fan-out.
- **Tenant override is a direct lookup, not a condition** — a `tenant_channel_override`
  table swaps a channel's provider list per tenant, avoiding per-tenant rule explosion.
  **Deferred to a later phase**; the resolution seam is designed now so adding it later is
  additive.
- **Send API accepts inline routing.** A caller may pass
  `{"routing": {"mode": "first|all", "channels": ["sms","email"]}}` to override channel
  selection + mode for that send (still subject to subscription opt-out). The **provider
  lists for those channels come from the matched routing policy** (config default if the
  policy doesn't cover a channel) — callers pick channels + mode, not providers.
- **New service named "Redelivery."** It owns fallback + scheduled re-attempts. Note the
  term overlaps NATS's own "redelivery" (the fast tier's consumer redelivery) — they are
  distinct: NATS redelivery is the in-broker fast retry; the Redelivery service is the
  central post-DLQ fallback/long-retry brain.
- **Retry ladder:** (1) fast provider retries via NATS-native redelivery; (2) fallback to
  the next provider via the Redelivery service; (3) long-horizon channel retry with exponential
  backoff up to a per-channel budget (default 24h). For `channel_mode=first`, **fast channel
  fallback happens first; the long retry runs only after the last channel is exhausted.**
- **Long-tier deferral is ack-and-reschedule, not in-flight `NakWithDelay`.** The fast tier
  uses NATS-native nak/backoff (few messages ever pending at once); the long tier acks the
  failure, persists a durable retry row, and a scheduled consumer re-publishes when due.
  This avoids `MaxAckPending` backlog during a provider outage, gives precise timing across
  restarts/leader changes, and keeps retry state queryable.
- **Retry-state store rides the existing store abstraction** (ADR 0001): native Postgres for
  the self-host default, the **DynamoDB model (via ExtendDB / native DynamoDB) at scale** —
  because a provider outage makes retry-state an unbounded, key-accessed, burst-write table,
  exactly the ADR's migration criterion. The schedule uses a **time-bucketed "due" index**
  with **sharded buckets + jitter** to avoid hot partitions, and TTL for give-up cleanup.
- **Jitter on every backoff step** (full/decorrelated jitter) — both to prevent synchronized
  retry storms after an outage recovers and to spread scheduled writes across buckets.
- **`delivery_attempts` is an append-only per-attempt log** — every attempt (success *and*
  interim failure) with provider, outcome, error, classification, and timing. It doubles as
  the provider-id→notification-id correlation index and the source for a delivery timeline.
- **Inbound correlation: a Hermes-owned Receipt Correlator** — providers emit
  provider-id-keyed receipts; correlation to `notification_id` happens in trusted code.
- **Push ingress: a single shared Inbound Gateway**; pull-style (SQS) providers run their
  own poller and skip it.
- **SDK: a public Go SDK only** (`hermesplugin`). Non-Go authors get the published wire
  schema (AsyncAPI/proto) and are on their own — no reference SDK in other languages.
- **Routing policies are editable both ways:** loaded from a config file (version-controlled
  defaults, GitOps) and editable at runtime via the Admin API/portal, with the same
  underlying model.
- **Token-gated self-enrollment.** Out-of-process providers self-register: the operator
  issues a one-time enroll token, the SDK presents it + the manifest to Admin on boot, and
  Hermes mints a **scoped NATS user JWT** (Hermes as account issuer). Self-registration, but
  operator-authorized — no process can grant itself creds.
- **Periodic health reports (per-replica).** Each of a provider's worker replicas heartbeats
  on `provider.health.<channel>.<provider>` with an `instance_id`; the **Event Writer (in a
  monitor role — no new service)** keeps per-instance membership and rolls up to per-provider
  state. **Liveness is OR across replicas** — down only when *all* replicas lapse; routing
  skips a provider with no live instances.
- **Circuit breaker (later phase), ground-truth authority.** Per-provider breaker state in
  Redis, consulted by dispatch when building the plan (open → skip provider). It trips on
  **error rate computed from `delivery.results.*`** (platform-owned, un-gameable under mixed
  trust); self-reported health is advisory only. Built on the results stream + health, so it
  sequences after them. (This reopens the former day-1 non-goal, now an explicit later phase.)

## Design

### 1. One contract, two hosting modes

The provider interface (today's `delivery.Provider`, lightly evolved) is the single seam.
*How* a provider is hosted is a deployment choice, not a code difference.

**In-process (built-in) — the default, easy path.** Built-in providers (ses, smtp, twilio,
sendgrid, inbox, …) are compiled into the delivery worker and self-register into a provider
registry. The operator enables a set per channel by config:

```
HERMES_EMAIL_PROVIDERS=ses,smtp     # 0 or more; empty = channel disabled
HERMES_SMS_PROVIDERS=twilio
```

The worker subscribes to the enabled providers' subjects and delivers. Zero extra
processes; `make dev-up` works out of the box.

**Out-of-process (third-party) — the extension path.** A third-party provider runs as its
own process/container using the **public `hermesplugin` Go SDK** (the only SDK we ship). The
SDK wraps NATS connection, subscription, the work queue, classified-error → ack/nack/term
mapping, and receipt emission. Non-Go authors implement against the **published wire schema**
(AsyncAPI/proto) directly — we don't provide a reference SDK in other languages. Either way it
connects with **scoped NATS credentials** (Section 6).

**Why one interface matters:** a built-in can be extracted to out-of-process (to isolate a
flaky provider or scale it independently) with no change to the provider's own code — only
to how it is launched. Mixed trust is handled by *where it runs* and *what creds it holds*,
not by two parallel APIs.

The provider contract is deliberately tiny — it never reasons about what happens next:

```go
type Provider interface {
    Channel() string                 // e.g. "email"
    Name() string                    // e.g. "ses"
    // Send attempts ONE delivery. ProviderID in the result is the carrier's id
    // (recorded for receipt correlation). Errors are classified transient vs permanent.
    Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
}
```

### 2. Channel vs. Provider model (de-hardcoding)

Both live in registries. A **Channel** declares: slug, the **content schema** it expects
(the fields a template must provide), and the **address requirement** (which contact-point
key it needs and how to validate presence). A **Provider** declares its channel plus
capabilities (Section 6).

The three hardcoded `switch ch` blocks become registry lookups:

| Today (hardcoded) | Becomes |
| --- | --- |
| `FilterChannelsForTemplate` — does this template have content for the channel? | "Does template content satisfy the channel's content schema?" |
| Contact filter — `email`→`Recipient.Email`, `sms`→`Recipient.Phone` | "Does the user have a contact point for the channel's required address key?" |
| `contentForChannel` — which rendered fields map to the channel | "Project rendered content onto the channel's content schema." |

Adding a channel = registering a channel descriptor (slug + content schema + address key),
not editing dispatch.

### 3. Normalized content & address model

Per-channel template content and recipient addresses move out of fixed columns into
channel-keyed tables. This is the largest single workstream and is sequenced first.

**Template content** → `template_channel_content`:

```
template_channel_content(
  template_id  references templates,
  channel_slug text,
  content      jsonb,        -- validated against the channel's declared content schema
  primary key (template_id, channel_slug)
)
```

Migrate existing `EmailSubject/EmailBody/SMSBody/InboxTitle/InboxBody` columns into rows
keyed by channel. Rendering (`RenderTemplates`, `contentForChannel`) reads from this table
and validates against the channel's content schema.

**Recipient addresses** → `user_contact_points`:

```
user_contact_points(
  user_id      references users,
  channel_slug text,
  address      text,
  verified     bool,
  primary key (user_id, channel_slug)
)
```

Migrate `users.email`/`users.phone` into rows. The `Recipient` struct and the dispatch
contact filter resolve contact points by the channel's required address key. Per-notification
overrides (`msg.Email`, `msg.Phone`) generalize to a channel-keyed override map.

This touches the templates table, rendering, the user model, `Recipient`, admin template
CRUD, and the SDKs/API — it is its own phase, landing before routing depends on it.

### 4. Routing & retry

This is where the design changed most. Two principles: **providers are dumb**, and
**routing/retry is central and failure-driven (fed by the DLQ).**

#### 4a. Routing policy

A policy is matched first-wins on optional conditions, and describes which channels to use
and, per channel, which providers — each with a `first | all` mode:

```
RoutingPolicy (ordered; first match wins)
  conditions:   optional equality predicates (category, region, attrs…)   # general rules
  channel_mode: first | all
  channels (ordered):
    - channel: email
      provider_mode: first | all
      providers:    [ses, smtp]      # ordered
    - channel: sms
      provider_mode: first
      providers:    [twilio]
```

- **channel_mode** — `all` fans out to every listed channel (today's behavior, the common
  case); `first` is channel failover (deliver via the first channel, fall back to the next).
- **provider_mode** — `first` is failover within the channel (first success wins; right for
  email — don't double-send); `all` fans out to every provider (right for webhooks —
  deliver to all endpoints).
- The **default policy** is the operator's enabled-provider config order with `channel_mode`
  and `provider_mode` defaults, so **zero policies still works**.
- **Cost** is an ordering input: a provider declares a cost tier in its manifest; a policy
  may pin order or prefer cheapest.

**Send-API inline routing.** A caller may pass a routing block to override channel selection
for a single send:

```json
{ "routing": { "mode": "first|all", "channels": ["sms", "email"] } }
```

`routing.channels` is the ordered channel set; `routing.mode` is the `channel_mode` (`first`
= failover across channels, `all` = fan-out). This carries on `SendMessage` into dispatch.
**Providers are not caller-selectable** (operators own provider choice) — the per-channel
provider lists + `provider_mode` come from the **matched routing policy**. Inline routing is
still subject to subscription opt-out for non-required categories, matching today's
explicit-channels behavior.

**Resolution order** (dispatch builds the DeliveryPlan):
1. **Match the policy** (first-match on conditions) → its channels, `channel_mode`, and
   per-channel `{providers, provider_mode}`.
2. **Apply Send-API `routing`** if present: replace the channel set + `channel_mode` with the
   send's, and for each send-specified channel take `{providers, provider_mode}` from the
   matched policy (config default if the policy doesn't cover that channel).
3. *(Later phase)* per-tenant override swaps a channel's providers.

**Tenant override — a direct lookup, not a condition (later phase).** General policies are
for broad rules; a tenant that wants its own email provider should not require encoding a
per-tenant condition (the "evaluate 10,000 conditions" problem). After a policy selects
channels + modes, each selected channel is checked against `tenant_channel_override`; if a
row exists for `(tenant, channel)`, it **swaps in** that tenant's provider list + mode. O(1),
no condition explosion. This is **deferred to a later phase** — the resolution step above
reserves the slot so adding it is purely additive.

```
tenant_channel_override(tenant_id, channel, provider_mode, providers[])   -- later phase
```

**Storage & editing.** Policies live in `routing_policies` and are editable two ways over the
same model: a **config file** (version-controlled defaults, GitOps-friendly, loaded on
startup) and the **Admin API/portal** for runtime changes. Config-file policies seed/sync on
boot; portal edits are the live source of truth thereafter.

Dispatch resolves Send-API routing → policy (→ tenant overrides, later phase) → a
**DeliveryPlan** (selected channels with modes and ordered provider lists), Redis-cached
like templates. The plan is embedded in the
`DeliveryMessage` so it survives into the DLQ — but **only the Redelivery service ever
reads or advances it**; providers never touch it.

```
DeliveryMessage {
  …existing fields…
  Channel        string
  ChannelMode    string      // first | all   (across the plan's channels)
  ChannelPlan    []string    // ordered channels, for channel_mode=first fallback
  ProviderMode   string      // first | all   (within this channel)
  ProviderPlan   []string    // ordered providers for this channel
  ProviderIdx    int         // current provider; starts at 0
  FirstAttemptAt time.Time   // for the long-retry budget
  IdempotencyKey string      // stable, passed to providers so retries don't double-send
}
```

#### 4b. Retry ladder

Three tiers, in order. Providers contribute nothing but a classified error.

1. **Provider retries — fast (NATS-native).** The provider returns a classified error;
   transient → **nack** (JetStream redelivers per a small `MaxDeliver` with a short
   `BackOff`), permanent (e.g. 4xx) → **term** (no retry). The provider never loops.
   Exhaustion/term routes the message to `dlq.delivery.<channel>.<provider>`.
2. **Fallback — Redelivery service.** A dedicated service consumes `dlq.delivery.*`.
   For `provider_mode: first`, it republishes to the next provider in `ProviderPlan`
   (`ProviderIdx+1`). For `all`, each provider is an independent ladder (nothing to fall
   back to).
3. **Channel retry — long-horizon.** When a channel's providers are exhausted, the retry
   service applies channel semantics:
   - **`channel_mode: first`** → **fast channel fallback first**: move to the next channel
     in `ChannelPlan` and run its provider ladder. Only after the **last** channel is
     exhausted does the long retry begin (restarting the channel ladder from the top).
   - The **long retry** schedules exponential-backoff re-attempts (e.g. 1m → 2m → 4m → …
     capped, with jitter) until a **per-channel budget (default 24h, configurable)** measured
     from `FirstAttemptAt` is spent → then terminal DLQ + fail notification.

**Long-tier mechanism — ack-and-reschedule (not `NakWithDelay`).** Fast provider retries
(tier 1) use NATS-native redelivery — few messages are ever simultaneously pending, so
holding them in-flight is cheap. The **long tier does not** hold messages in-flight: the
Redelivery service **acks** the failed message, persists a durable retry row, and a scheduled
worker re-publishes a fresh delivery message when due.

This is chosen over JetStream `NakWithDelay` for the long horizon because nak-delay keeps
each message as an outstanding ack-pending message for the *whole* delay: a provider outage
would pile thousands of 24h-deferred messages against `MaxAckPending` and stall the consumer
exactly when it's needed; nak-delay timing is best-effort (resets on restart / consumer
leader election, risking a post-recovery thundering herd); and in-flight messages aren't
queryable.

**Retry-state store — built for outage-scale.** A provider outage turns retry-state into a
high-volume, key-accessed, burst-write table — precisely ADR 0001's criterion for the
DynamoDB model. So `delivery_retries` rides the **existing store abstraction**: native
Postgres for the self-host default, the **DynamoDB model (ExtendDB / native DynamoDB) at
scale**. The scheduler reads it as a **time-bucketed due-queue**, not a full-table scan:

```
# Postgres shape (self-host default)
delivery_retries(
  notification_id text,
  channel         text,
  next_attempt_at timestamptz,    -- (sharded-bucket index) scheduler scans due rows
  attempt         int,
  first_attempt_at timestamptz,   -- budget anchor; give up past first_attempt_at + budget
  plan            jsonb,          -- the DeliveryPlan to re-publish
  primary key (notification_id, channel)
)

# DynamoDB-model shape (scale)
PK: RETRY#<notification_id>   SK: CHAN#<channel>          # O(1) get/update/delete
GSI "due":  PK = due_bucket ("<epoch-minute>#<shard>")   # scheduler queries due buckets
            SK = next_attempt_at (RFC3339)
ttl (N)  = first_attempt_at + budget                     # native give-up cleanup
```

**Avoiding the hot partition.** A burst of failures all rescheduled to the same minute would
hammer one `due_bucket` partition. Two mitigations: (1) the bucket key carries a **shard
suffix** (`<minute>#0..N`); the scheduler fans out a small query per shard per due minute.
(2) **Jitter** on each backoff step spreads `next_attempt_at` across many buckets so the herd
de-synchronizes as it backs off. TTL handles give-up cleanup without a sweep job.

Ack-and-reschedule thus trades a small scheduler for no in-flight backlog, precise durable
timing, jitter, `SELECT`/`Query`-able retry state, and a horizontal scale path that matches
the rest of the hot data path.

**One service, two roles — no separate scheduler deployable.** The Redelivery service runs
both the DLQ consumer (event-driven fallback) and the due-queue scheduler (the long tier) in
the same binary. Both scale horizontally and stay safe under multiple replicas: the DLQ
consumer via a NATS queue group; the scheduler via **concurrency-safe claiming** of due rows
— Postgres `SELECT … FOR UPDATE SKIP LOCKED`, or a conditional claim (guarded `UpdateItem`)
on the DynamoDB path — so two replicas never re-publish the same retry. No leader election
required.

**Idempotency:** the stable `IdempotencyKey` is passed into every provider `Send` so fast
redelivery (or a fallback that races a slow success) cannot double-send through carriers
that honor it.

**Trust simplification (consequence of dumb providers):** because no provider ever
republishes to a sibling subject, the earlier "capability-gated failover / failover reaper"
machinery is gone. The Redelivery service is the *only* thing that advances plans.
Trusted and untrusted providers have the **identical, minimal** cred surface.

#### 4c. Recording attempts (how interim failures are captured)

Every send outcome — accepted *or* failed, including each fast-retry attempt — is recorded,
without breaking dumb providers or the trust boundary:

- The provider's **SDK** wraps `Send`: it maps the result to NATS ack/nack/term (fast-retry
  control) **and** publishes a structured **delivery result** to
  `delivery.results.<channel>.<provider>` — a subject the provider is permitted to publish to.
  The provider author writes nothing; the SDK emits it, identically for built-in and
  third-party hosting.
- The result carries the `notification_id` (the provider already has it from the
  `DeliveryMessage` it consumed), `channel`, `provider`, `attempt_no` (from the NATS delivery
  count), `outcome` (accepted | transient_failed | permanent_failed), `provider_id` (when the
  carrier accepted), `error`, and `latency_ms`.
- The **trusted Event Writer** consumes `delivery.results.*`, writes one `delivery_attempts`
  row per message, and advances notification status (sent / failed) in the rollup. Because a
  provider can only publish under its own `…<provider>` subject, it can't claim outcomes for
  another provider; the Event Writer remains the only thing that touches the DB or
  `notification.events`.

So interim failures are recorded **from the providers' own per-attempt result messages — not
from DLQ writes** (the DLQ is purely the Redelivery service's fallback trigger). And because
send-time results already carry `notification_id`, **correlation (§5) is needed only for the
later async carrier receipts**, which arrive keyed by `provider_id` alone.

### 5. Inbound receipts (the event hook)

Providers report status asynchronously and out of band. Four pieces:

**(a) Ingress — two styles, declared in the manifest.**
- **Pull** (e.g. SES): the provider runs its own SQS poller (an SDK helper). No public
  surface; the queue lives in the operator's account. The common case.
- **Push** (e.g. Twilio): routed through a single shared **Inbound Gateway** at
  `/inbound/<channel>/<provider>`. It authenticates the caller and forwards the raw payload
  to the provider over NATS for verification + normalization. One public TLS / attack
  surface for the platform; third-party providers never expose their own port.

**(b) Normalization.** The provider (the only thing that understands the carrier's payload)
maps it to a normalized receipt and publishes to `delivery.receipts.<channel>.<provider>`:

```
DeliveryReceipt {
  Provider   string
  ProviderID string   // the carrier's id, NOT Hermes's notification id
  Status     string   // delivered | bounced | complained | failed | …
  Timestamp  time.Time
  Reason     string
  Raw        json.RawMessage
}
```

**(c) Correlation — protects the trust boundary.** An async carrier receipt arrives keyed by
`provider_id` only (the carrier doesn't know Hermes's `notification_id`). A Hermes-owned
**Receipt Correlator** consumes `delivery.receipts.*`, resolves `provider_id →
notification_id` via the `delivery_attempts` index (populated at send time by §4c — the
accepted attempt carries the `provider_id`), and emits a proper `EventMessage`. **Third-party
providers never touch the database** — they only state "provider X bounced"; identity is
resolved in trusted code.

`delivery_attempts` is the **append-only per-attempt log** written by the Event Writer from
the §4c `delivery.results.*` stream — one row per attempt, successes *and* interim failures
(provider A failed → fell back to B → succeeded is three rows). It serves three purposes:
provider-id→notification-id correlation for async receipts, the full failover/retry history,
and the data behind a delivery timeline. **Third-party providers never write it.**

```
# Postgres shape
delivery_attempts(
  id              text primary key,   -- attempt id
  notification_id text,
  channel         text,
  provider        text,
  attempt_no      int,
  outcome         text,        -- accepted | transient_failed | permanent_failed
  provider_id     text,        -- nullable; set when the carrier accepted (correlation key)
  error           text,        -- nullable; failure detail
  latency_ms      int,
  created_at      timestamptz,
  -- index (notification_id, created_at)  for the timeline / history
  -- index (provider, provider_id)        for receipt correlation
)

# DynamoDB-model shape
PK: NOTIF#<notification_id>   SK: ATTEMPT#<created_at>#<attempt_no>   # timeline Query
GSI "by-provider-id": PK = "<provider>#<provider_id>"                # receipt correlation
```

Failures are *also* already on `notification.events` for the rollup stream; `delivery_attempts`
is the keyed, queryable record (events aren't indexed by `provider_id`). The Receipt
Correlator looks up `notification_id` via the `(provider, provider_id)` index.

**(d) Reuse the existing rollup.** The correlator's `EventMessage`s land on
`notification.events`, which the **Event Writer already consumes and rolls up**
(pending→sent→delivered→…). The only addition is slotting new states (`bounced`,
`complained`) into the status rank model.

### 6. Manifest, registration & trust scoping

**Manifest.** Every provider declares: `id` + `version`, the `channel` it serves,
`capabilities` (produces_receipts, ingress style pull/push, cost tier, content features),
and a `config schema` (settings/secrets needed). Subjects are derived:
`delivery.<channel>.<provider>` (consume), `delivery.receipts.<channel>.<provider>`
(produce).

**Two registration paths:**
- **Built-in (first-party):** registered in the registry at compile time (manifest is a Go
  struct), enabled by the Section 1 config. Trusted, full creds, in-process.
- **Third-party — token-gated self-enrollment.** The operator issues a one-time **enroll
  token** in Admin (bound to the intended channel/provider). The provider starts with
  `{admin_url, enroll_token, nats_url}`; the **SDK enrolls on boot** — `POST
  /providers/enroll` with the token + manifest. Admin validates the token, registers the
  manifest in the `providers` table, and **mints a scoped NATS user JWT** (Hermes as the NATS
  account issuer) with subject permissions derived from the manifest. The SDK connects to
  NATS with it. HTTP-to-Admin is the bootstrap channel precisely because the provider has no
  NATS creds yet; tokens are single-use; rotation/revocation ride NATS JWT expiry + account
  update.

  The minted creds let it subscribe to its own `delivery.<channel>.<provider>` and publish to
  its own `delivery.results.…` (per-attempt outcomes, §4c), `delivery.receipts.…` (async
  carrier callbacks), and `provider.health.<channel>.<provider>` (heartbeat, §7). It
  **cannot** publish to `notification.events` (only the Event Writer / correlator can) or to
  sibling delivery subjects (only the Redelivery service advances plans). A rogue provider can
  only report outcomes under its own provider name — it cannot fabricate status for another
  provider, misroute, or write the DB.

**Dispatch discovery.** Built-ins seed the registry at startup; third-party manifests live
in a `providers` table; dispatch reads the merged, Redis-cached view to build DeliveryPlans.

**Config & secrets.** Provider config resolves tenant→global with fallback (the per-tenant
seam). Built-in secrets via env day 1; third-party secrets via referenced secret. Per-tenant
secrets are future.

### 7. Provider health & circuit breaking

Two related lifecycle signals, both feeding the routing path (distinct from OTel/Prometheus,
which observe but don't steer routing).

**Health reports (phase 7).** A provider runs as **multiple worker replicas** (the queue
group on `delivery.<channel>.<provider>`), so each replica heartbeats independently — the
monitor role sees *N* reports per provider, not one. Each heartbeat on
`provider.health.<channel>.<provider>` therefore carries an **`instance_id`** (plus status,
rolling success/fail counts the SDK computes from wrapping `Send`, queue depth, version,
uptime).

The **Event Writer takes on a monitor role** (it already consumes the delivery streams — see
the topology note at the end of this section) and maintains **per-instance membership** —
e.g. a Redis key `provider:<p>:instance:<id>` with a TTL of a few heartbeat intervals —
rolled up to per-provider state in the `providers` registry. Aggregation rules:
- **Liveness is OR across instances:** a provider is "down" only when its live-instance set
  is **empty** (all replicas lapsed). One healthy replica keeps it routable. (A single sick
  replica is an ops/readiness concern — it fails its own readiness probe and stops consuming
  the queue group — *not* a routing decision, since the breaker can't single out one replica
  of a shared subject.)
- **Self-reported counts are summed** across live instances (advisory only).
- The membership set also gives a **fleet/version view** for free (replica count, mixed
  versions during a rolling deploy).

**Built-in providers heartbeat too.** Health is emitted by the shared SDK path regardless of
hosting — enrollment (§6) governs only NATS creds for *out-of-process* providers, not health.
The built-in delivery worker uses the same in-process SDK, so its replicas report on the same
`provider.health.*` subjects with `instance_id`s. The monitor role therefore has a
**uniform view of every provider, first- and third-party** — which is what the admin portal
renders (below).

**Circuit breaker (phase 9).** Per-provider breaker state (`closed → open → half-open`) lives
in **Redis**, consulted by dispatch during plan construction:
- **Open** → the provider is dropped from the plan's ordered list; for `provider_mode=first`
  the plan starts at the next provider, for `all` it's excluded from the fan-out. If *every*
  provider for a channel is open, the channel goes straight to long retry (which lets the
  breaker half-open during the wait).
- **Trip authority is ground-truth — and naturally replica-aggregate.** Every replica's
  attempts land on the same `delivery.results.<channel>.<provider>` subject, so the Event
  Writer's rolling outcome counters are already **per-provider, summed across all replicas** —
  no per-instance aggregation needed (unlike heartbeats). The monitor role flips the breaker
  when the windowed error rate exceeds a threshold (with a minimum-volume guard).
  **Self-reported health is advisory only**, and an empty live-instance set is a liveness-down
  input — neither can be gamed by a malicious provider to *keep* traffic, because the
  authoritative signal is the platform's own outcome data. The breaker is **provider-level**
  (it can't target one replica of a shared subject — that's what readiness probes are for).
- **Half-open** after a cooldown: a bounded number of probe sends are allowed through
  (gated by a Redis atomic counter so replicas don't all probe at once); success → `closed`,
  failure → `open` with a fresh cooldown.

Breaker thresholds (error-rate %, window, min-volume, cooldown, probe count) are configurable
per provider/channel in the routing config. Built-in providers participate identically — the
SDK path is shared.

**Topology — no new service.** The "monitor role" is **folded into the Event Writer**, not a
standalone deployable. The Event Writer is already the sole consumer of `delivery.results.*`
and owns the outcome counters, so it adds a `provider.health.*` subscription (low frequency),
the TTL membership keys, and the breaker-evaluation loop. All derived state (liveness, fleet,
breaker) lands in **Redis**, which dispatch reads when planning and Admin reads for the
dashboard. Because that state lives in Redis behind a clean boundary, extracting a dedicated
`provider-monitor` service later (if breaker logic grows) is a move, not a rewrite.

## New & changed components

- `internal/provider/` (new): `Provider` interface, manifest types, registry, channel
  descriptors.
- `internal/routing/` (new): routing-policy model + first-match evaluation, tenant-override
  resolution, `DeliveryPlan` builder.
- `internal/redelivery/` + **Redelivery service** (`cmd/redelivery`, new): consumes
  `dlq.delivery.*`; provider fallback, channel fallback, and the long-horizon retry —
  ack-and-reschedule via `delivery_retries` (store abstraction; Postgres or DynamoDB-model)
  + a scheduler that fans out across due buckets/shards and re-publishes; give-up past
  budget via TTL. The single owner of plan advancement.
- `hermesplugin` SDK (new, **public Go module**): NATS plumbing, work queue, classified-error
  → ack/nack/term, receipt emission, SQS-poller and webhook helpers — for built-ins and
  out-of-process plugins alike. Non-Go authors target the published AsyncAPI/proto schema.
- `internal/dispatch/`: replace `switch ch` blocks with channel-registry lookups; resolve
  Send-API inline routing → policy (tenant overrides later); embed `DeliveryPlan`; publish to
  `delivery.<channel>.<provider>`.
- Send API + `SendMessage`: add the inline `routing` block (`mode` + `channels`).
- Delivery worker(s): consume per-provider subjects; host enabled built-ins via the
  in-process SDK; map classified errors to nack/term (no retry/fallback logic); emit
  `delivery.results.*` (phase 3) and `provider.health.*` heartbeats (phase 7) like any
  provider. The existing `worker-email`/`worker-sms`/`worker-inbox` are updated to this path.
- `internal/inbound/` + **Inbound Gateway** (`cmd/inbound-gateway`, new): shared push
  ingress.
- **Receipt Correlator** (new consumer): `delivery.receipts.*` → resolve `notification_id` →
  `notification.events`.
- Event Writer: consume `delivery.results.*` → write append-only `delivery_attempts`
  (successes + interim failures) + advance status; maintain per-provider rolling outcome
  counters in Redis; extend status rank with `bounced`/`complained` (from correlator events).
  **Plus a monitor role (no new service):** consume `provider.health.*`, keep per-instance
  membership (TTL'd Redis keys) → per-provider liveness (OR across replicas), and evaluate
  breaker state → Redis for dispatch to read. (Health = phase 7; breaker = phase 9; both
  extractable to a dedicated `provider-monitor` later since state is in Redis.)
- Admin: `POST /providers/enroll` (token → mint scoped NATS user JWT) + enroll-token issuance;
  Hermes as NATS account issuer.
- `hermesplugin` SDK: enroll-on-boot, heartbeat emitter, and the shared `Send`-wrapping that
  emits `delivery.results.*` + maps ack/nack/term.
- Schema (all hot tables ride the store abstraction — Postgres or DynamoDB-model):
  `template_channel_content`, `user_contact_points`, `delivery_attempts`, `delivery_retries`,
  `providers` (registered third-party), `routing_policies`; `tenant_channel_override` (later
  phase); migrate existing columns.
- Admin API + portal: provider registration/manifest install, scoped-cred provisioning,
  routing-policy CRUD + config-file policy loader (seed/sync on boot); a **provider health
  dashboard** (read API `GET /providers` with rolled-up status + fleet/version + breaker
  state, for **all** providers — built-in and third-party). Tenant-override CRUD lands with
  the later tenant-override phase.

## NATS subjects

| Subject | Direction | Notes |
| --- | --- | --- |
| `delivery.<channel>.<provider>` | dispatch / Redelivery service → provider | per-provider work queue; fast retries via consumer `MaxDeliver` + `BackOff`; exhaustion → DLQ |
| `dlq.delivery.<channel>.<provider>` | provider exhaustion → Redelivery service | feedback channel for fallback + long retry |
| `delivery.results.<channel>.<provider>` | provider (SDK) → Event Writer | per-attempt send outcomes (has `notification_id`); writes `delivery_attempts` + status |
| `delivery.receipts.<channel>.<provider>` | provider → Receipt Correlator | async carrier callbacks, `provider_id`-keyed |
| `provider.health.<channel>.<provider>` | each provider replica (SDK) → Event Writer (monitor role) | periodic heartbeat w/ `instance_id`; per-instance membership → provider liveness + advisory health |
| `notification.events` | Event Writer / correlator → rollup | existing; providers cannot publish here |

## Sequencing (phases)

1. **Channel/provider registries + de-hardcoding** — interface, registry, replace switches.
   No behavior change (existing channels/providers registered as built-ins).
2. **Normalized content & address model** — `template_channel_content`,
   `user_contact_points`, migrate columns, update rendering. (Largest; gates later work.)
3. **Routing + DeliveryPlan** — `routing_policies` (config + portal), Send-API inline
   routing, first|all semantics, per-provider subjects + DLQ partitions, per-attempt
   `delivery.results.*` → Event Writer → append-only `delivery_attempts`.
4. **Redelivery service** — DLQ-fed fallback, channel fallback, ack-and-reschedule long
   retry (`delivery_retries`, bucketed/sharded scheduler) with budget + jitter,
   idempotency-key plumbing.
5. **Inbound receipts** — Receipt Correlator, `delivery.receipts.*`, Event Writer status
   states, SES SQS pull path as the first consumer.
6. **Inbound Gateway (push)** + first push provider (e.g. Twilio status).
7. **Third-party plugin path + health** — `hermesplugin` SDK, token-gated self-enrollment
   (Admin enroll endpoint + scoped NATS JWT minting), **health reports** for *all* providers
   incl. built-ins (`provider.health.*` + Event Writer monitor role + liveness-based skipping), and the
   **admin portal provider health dashboard**.
8. **Per-tenant channel overrides** — `tenant_channel_override` table + resolution step +
   tenant-override CRUD (the seam reserved in phase 3).
9. **Circuit breaker** — Redis breaker state, ground-truth trip from rolling
   `delivery.results.*` counters, half-open probes, dispatch consults state when planning.

## Trust model summary

| Concern | Built-in (trusted) | Third-party (untrusted) |
| --- | --- | --- |
| Hosting | in-process, compiled in | separate process/container |
| NATS creds | full | scoped: consume own delivery subject; publish own results/receipts/health |
| Onboarding | compiled in + config | token-gated self-enrollment → minted scoped NATS JWT |
| DB access | yes | never (correlator resolves identity) |
| Public ingress | n/a | only via shared Inbound Gateway |
| Emit `notification.events` | n/a (correlator does) | denied |
| Advance routing/retry plan | never (Redelivery service does) | never (Redelivery service does) |
| Health self-report | advisory only | advisory only (ground-truth `delivery.results` is authoritative) |

Routing/retry being fully central means trusted and untrusted providers have the **same
minimal** cred surface — the boundary is enforced by NATS permissions, not provider code.

## Testing strategy

- **Unit:** registry lookups; Send-API routing → policy first-match resolution order;
  first|all plan construction (channel and provider level); retry-ladder transitions
  (provider fallback, channel fallback, long-retry scheduling, budget exhaustion);
  bucket/shard assignment + jitter distribution; append-only `delivery_attempts` recording
  interim failures; receipt normalization; content-schema validation. Mock store interfaces
  per existing pattern.
- **Integration** (`//go:build integration`, both store backends per ADR 0001): per-provider
  subject fan-out, fast retries via `MaxDeliver`/`BackOff`, DLQ partitioning, Redelivery
  fallback + ack-and-reschedule long retry (due-bucket query fires correctly, TTL give-up),
  correlation via `delivery_attempts`, Event Writer rollup with `bounced`/`complained`.
- **E2E** (`tests/e2e/`): send → route → deliver with `provider_mode=first` (primary fails
  → fallback to secondary) and `=all` (fan-out) → inbound receipt (SQS pull) → status
  rollup; Send-API inline `routing` override; `channel_mode=first` channel fallback; a fake
  third-party provider that **self-enrolls** (token → scoped creds) over NATS, validating the
  trust boundary (cannot publish events or sibling subjects); breaker trips on a high
  ground-truth error rate → dispatch skips the open provider → half-open probe closes it.
- **Breaker/health unit:** rolling error-rate windowing + min-volume guard; closed→open→
  half-open transitions; **per-instance membership roll-up** (liveness OR across replicas —
  down only when all lapse, multi-replica heartbeats don't double-count); self-report stays
  advisory.

## ADRs to write (alongside implementation)

This is architecturally significant (new cross-service contract + messaging subjects + a
new service). Per `CLAUDE.md`, write ADRs in `docs/adr/` in the same PRs:

- Provider plugin model & bus-native isolation (vs gRPC/WASM).
- Normalized per-channel content & contact-point model (supersedes fixed template columns).
- Central routing/retry with first|all semantics and the Redelivery service.
- Inbound receipt pipeline & correlation boundary.
- Provider lifecycle: token-gated self-enrollment (Hermes as NATS account issuer), health
  reports, and the ground-truth circuit breaker.

## Open questions

- Bucket granularity + shard count for the `delivery_retries` "due" index — tune against the
  worst-case outage burst (per-minute buckets × N shards); load-test before fixing N.
- Config-file vs portal precedence on conflict: confirm "portal is live source of truth
  after boot; config seeds/syncs" is the desired reconciliation (vs config always wins).
- Surfacing the `delivery_attempts` timeline in the admin portal / user API — the data now
  exists; whether to build the view is a sequencing call (likely a small follow-up phase).
- Enroll-token lifecycle: TTL, single-use vs reusable for a replica set, and re-enrollment on
  JWT expiry (re-use original token vs rotate creds with existing creds).
- Breaker half-open probe coordination across dispatch replicas — confirm a Redis atomic
  counter is sufficient vs a more formal lease.
