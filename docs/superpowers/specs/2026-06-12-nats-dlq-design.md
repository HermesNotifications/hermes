# NATS Dead-Letter Queue

## Context

When a JetStream consumer exhausts its delivery attempts, the message is silently lost.
`internal/messaging/nats.go` configures every consumer with `MaxDeliver: 10`; a message
that fails all ten attempts is never redelivered and lingers invisibly in its WorkQueue
stream until the 7-day `MaxAge` discards it. Handlers that return a `PermanentError`
(e.g., malformed payload, invalid tenant UUID) have their message `Term()`'d — deleted
immediately, equally invisible. Either way, a notification disappears with no metric, no
alert, and no way to inspect or replay it.

This design adds a dead-letter queue: terminal failures are captured to a dedicated
stream with diagnostic context, counted, alerted on, and documented in a runbook with a
replay procedure.

Decisions made during brainstorming:

- **Both drop paths are captured** — retry-exhausted and permanently-terminated —
  distinguished by a `reason` field/label.
- **Replay is manual** via the `nats` CLI, documented in the runbook. No `hermes dlq`
  CLI subcommand or admin UI for now.
- **Retention: 7 days / 1 GiB** (discard-old), matching the source streams' `MaxAge`.

The advisory-based alternative (consuming `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>`)
was rejected: `Term()` deletes the message before any advisory consumer could copy it, so
it cannot capture the permanent-error path, and it loses the handler's error context.

## Design

### Approach: capture in the consumer wrapper

All consumption flows through `Client.Subscribe` in `internal/messaging/nats.go`, which
already sees every failure with full context: payload, attempt count, consumer name, and
the handler's error. On terminal failure it publishes a dead-letter envelope to the `DLQ`
stream and then `Term()`s the original. One code path covers all nine services with no
per-service changes.

### 1. DLQ stream

Added to stream setup (`SetupStreams`), but **not** to the `Streams` slice that
`Subscribe` uses for subject→stream lookup — nothing consumes the DLQ in-process, and
keeping it out prevents accidental `Subscribe("dlq.…")` consumers:

```go
jetstream.StreamConfig{
    Name:      "DLQ",
    Subjects:  []string{"dlq.>"},
    Retention: jetstream.LimitsPolicy,   // survives reads; age/size-bounded
    Storage:   jetstream.FileStorage,
    MaxAge:    7 * 24 * time.Hour,
    MaxBytes:  1 << 30,                  // 1 GiB
    Discard:   jetstream.DiscardOld,
}
```

`LimitsPolicy` (not WorkQueue) because dead letters must survive inspection reads and
support multiple ephemeral consumers during an incident.

### 2. Dead-letter envelope

New struct in `internal/nats/` (package `hermenats`), alongside `SendMessage` /
`DeliveryMessage` / `EventMessage`:

```go
// DeadLetter wraps a message that exhausted its delivery attempts or was
// permanently rejected. Published to "dlq.<original subject>".
type DeadLetter struct {
    Subject   string          `json:"subject"`    // original subject, e.g. "delivery.email"
    Stream    string          `json:"stream"`     // source stream, e.g. "DELIVERY"
    Consumer  string          `json:"consumer"`   // durable consumer name
    Reason    string          `json:"reason"`     // "max_deliveries" | "terminated"
    Attempts  uint64          `json:"attempts"`   // delivery attempts consumed
    Error     string          `json:"error"`      // handler error string from the final attempt
    FailedAt  time.Time       `json:"failed_at"`  // RFC3339
    Payload   json.RawMessage `json:"payload"`    // original message body, verbatim
}
```

Published to `dlq.<original subject>` (e.g., `dlq.delivery.email`) so operators can
filter by pipeline stage with subject wildcards.

### 3. Capture logic in `Subscribe`

The handler-error branch in `internal/messaging/nats.go` becomes:

| Condition | Action |
|---|---|
| `PermanentError` | publish envelope (`reason=terminated`), then `Term()` |
| transient error, attempt ≥ `maxDeliveries` | publish envelope (`reason=max_deliveries`), then `Term()` |
| transient error, attempt < `maxDeliveries` | `NakWithDelay(retryDelay(attempt))` — unchanged |
| success | `Ack()` — unchanged |

The retry-exhausted path currently ends with a pointless final `Nak`; the explicit
`Term()` after a successful DLQ publish removes the dead message from the WorkQueue
stream immediately instead of letting it linger until `MaxAge`.

**DLQ publish failure:** never destroy a message we failed to preserve. If the envelope
publish fails, skip the `Term()` and `Nak` instead (on the true last attempt the Nak is
a no-op and the message lingers in the source stream as today — no worse than current
behavior). For `PermanentError` this means the handler may run again on a later attempt;
that is acceptable because pipeline handlers are idempotent by design (Postgres unique
indexes / DynamoDB conditional writes). Log the failure and increment the failure
counter (§4).

The DLQ publish uses the existing `Publish` method (trace-context injection included) so
the dead letter links to the original processing trace.

### 4. Metrics

OTel instruments in `internal/messaging`, per the semantic conventions (bounded labels —
stream, consumer, and reason are all small fixed sets):

- `hermes.messaging.dead_letters` (counter) — attributes `stream`, `consumer`, `reason`.
- `hermes.messaging.dlq_publish_failures` (counter) — attributes `stream`, `consumer`.

### 5. Alert rules + runbook (shipped together)

`deploy/observability/base/prometheus-rules/pipeline.rules.yaml`:

- `HermesDeadLetterDetected` (warning): `increase(hermes_messaging_dead_letters_total[15m]) > 0`
  — any dead letter is abnormal and worth a look; 15m window batches bursts into one alert.
- `HermesDLQPublishFailure` (critical): `increase(hermes_messaging_dlq_publish_failures_total[5m]) > 0`
  — messages are being lost *and* the safety net is failing.

Both annotate `runbook: docs/observability/runbooks/dead-letter-queue.md`. The runbook
covers:

1. **Triage** — which stream/consumer/reason is firing; Grafana panel to check.
2. **Inspect** — `nats consumer add DLQ inspect --pull --filter 'dlq.>'` then
   `nats consumer next DLQ inspect`; reading the envelope (error string, attempts).
3. **Replay** — `nats pub <envelope.subject> '<envelope.payload>'`; safe to replay
   because every pipeline stage is idempotent (dedup keys / conditional writes).
   `reason=terminated` messages need the underlying defect fixed first — replaying
   reproduces the permanent error.
4. **Purge** — `nats stream purge DLQ --subject dlq.<subject>` after resolution.

### 6. Testing

- **Unit** (`internal/messaging`): table-driven tests for the terminal-failure branching
  — permanent error → envelope with `reason=terminated` + Term; exhausted retries →
  `reason=max_deliveries` + Term; transient mid-flight error → Nak, no envelope; DLQ
  publish failure → no Term. Envelope field population (subject, consumer, attempts,
  error, payload passthrough).
- **Integration** (`//go:build integration`, real NATS via `make infra-up`): a handler
  that always fails consumes a published message; assert exactly one dead letter lands
  on the DLQ stream with `reason=max_deliveries` and `attempts=10`, and the source
  stream is empty. A second case with a `PermanentError` handler asserts
  `reason=terminated` after one attempt.

## Out of scope

- `hermes dlq` CLI subcommand and admin-portal DLQ views (revisit if the runbook
  procedure proves painful).
- Automated replay or poison-message quarantine policies.
- Dead-lettering for Centrifugo publish failures outside the NATS consumer path.
