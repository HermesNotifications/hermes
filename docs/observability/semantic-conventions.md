# Semantic Conventions

Naming rules enforced at code review. If a metric/span/log violates these, the PR is blocked.

## Metric names

- **snake_case**, dotted namespace. Example: `hermes.notifications.sent` → emitted as `hermes_notifications_sent_total` after Prometheus mapping.
- **Unit suffix matters.** Use the OpenTelemetry unit string (`s`, `ms`, `bytes`, `1`). The Prometheus exporter appends `_total` to counters and unit suffix to histograms automatically — don't include them in the OTel-side name.
- **No verbs in the name.** `hermes.requests.in_flight` — not `hermes.count_requests`. The type (counter/gauge/histogram) tells you what it does.
- **One per concept.** Don't emit a counter AND a histogram for the same thing with different names. Histograms imply a count — just use the histogram.

## Metric attributes (labels)

| OK | Why |
|---|---|
| `channel` (email/sms/inbox) | Bounded set, small |
| `http_route` | Bounded to your routes |
| `http_method` | GET/POST/PUT/DELETE/PATCH |
| `http_response_status_code` | Small integer range |
| `service` | Bounded, one value per service |
| `stream` / `consumer` (NATS) | Bounded, operator-owned |

### Forbidden high-cardinality labels

These kill Prometheus. **Never** put them on metrics. They belong on spans (as attributes) or in Loki (as log fields).

- `user_id`
- `notification_id`
- `organization_id`
- `trace_id`, `span_id` (these are separate, carried as exemplars)
- `request_id`
- `template_id` (unless we're sure set size stays <100)
- Full URL paths with IDs (use `http_route` pattern instead)
- IP addresses, hostnames (unless a fixed small set like infra nodes)
- Free-form user input of any kind

The OTel Collector's `attributes/metrics` processor strips the most common offenders (`user_id`, `notification_id`, `organization_id`) as a backstop, but don't rely on it — flag these in review.

## Span names

Format: `<noun>.<verb>`, lowercase, dots between levels.

| Good | Bad |
|---|---|
| `notification.publish` | `PublishNotification` |
| `template.resolve` | `resolve_template` |
| `user.fetch-preferences` | `GET /users/:id/prefs` |
| `db.query.notifications-by-user` | `SELECT * FROM notifications...` |

HTTP and DB spans are auto-named by instrumentations — don't override unless the auto name is unhelpful.

## Span attributes

Follow the [OTel semantic conventions](https://opentelemetry.io/docs/specs/semconv/) where they exist:

- HTTP: `http.request.method`, `http.route`, `http.response.status_code`
- DB: `db.system`, `db.statement`, `db.name`
- Messaging (NATS): `messaging.system=nats`, `messaging.destination`, `messaging.operation` (publish/receive/process)
- User-facing: put user IDs on spans as `user.id` — Tempo handles high cardinality fine

## Resource attributes

These describe **who** is emitting, not **what** they're emitting. Set once at SDK init via `observability.Init`, not per-call.

Required on every service:

- `service.name` — `hermes-send`, `hermes-dispatch`, etc.
- `service.version` — commit SHA or semver
- `deployment.environment` — `local`, `staging`, `production`
- `k8s.namespace.name` — injected by the Collector's resource processor
- `k8s.cluster.name` — injected by the Collector

## Log attributes

slog JSON output. Required fields:

- `time` (auto)
- `level` (auto)
- `msg` (auto — keep short, descriptive, NOT a sentence)
- `service` (injected by `observability.Init`)
- `trace_id` / `span_id` (auto, only if ctx has a span)

Domain fields: add as slog attrs with snake_case keys. Stable across the service lifetime — don't rename freely, dashboards key off them.

```go
slog.InfoContext(ctx, "notification accepted",
    slog.String("channel", "email"),
    slog.String("recipient_hash", h),
    slog.Int("template_version", v),
)
```

### Don't log

- Raw recipients (email/phone) — hash them
- Auth tokens / API keys of any kind
- Full request bodies unless explicitly flagged as safe
- Stack traces at info or warn level — only at error

## Log levels

The rule, and the only one that matters at production volume:

> **If it fires once per request, per notification, or per message, it is `debug`.**

Not because those events are uninteresting, but because a log line is the most expensive
way to record something that happens N times a second, and the two cheaper records are
already there: a **span** carries the per-occurrence detail with sampling built in, and a
**counter** carries the rate for free. A log record is for the thing you would want to read
a sentence about — which, in steady state, is nothing.

The consequence to design for: **at `info`, a healthy service is nearly silent regardless of
traffic.** Volume should track incidents, not throughput. If turning traffic up turns your
log volume up proportionally, something is misleveled.

| Level | For | Fires |
|---|---|---|
| `error` | The service failed at something it is responsible for, and someone should look. Retries exhausted, not retries attempted. | Per incident |
| `warn` | Running degraded, or a caller was refused. A dependency is sick but the fallback held; a request got a 4xx. | Per incident, or per request but **throttled** |
| `info` | Lifecycle. Startup, configuration resolved, shutdown, leader elected, stream provisioned. | Per process, or per operator action |
| `debug` | Everything per-request, per-notification, per-message — including the success paths. | Per unit of work |

### Rules

- **Success paths on hot code are `debug`.** "delivery succeeded", "published to delivery",
  "flushed events", a 2xx access log. All of these were `info` and all of them scaled with
  traffic.
- **Expected outcomes are not `warn`.** A user with no phone number, a category the user
  opted out of, an idempotency hit on a redelivery — these are the rules working. `warn`
  means *someone might need to act*, and a warning nobody can act on trains people to
  ignore warnings. Count them instead.
- **Retries are `warn`; exhaustion is `error`.** Logging every attempt at `error` turns one
  flaky provider call into a burst on every error-rate dashboard.
- **A per-request log about a sick dependency must be throttled.** Otherwise the outage
  itself becomes the log flood, at the worst possible moment. Use
  `observability.LogThrottle`, which emits once per interval and reports `suppressed=N`.
  Pair it with a counter — the counter carries the rate, the log carries the fact.
- **`msg` is a constant.** Never `fmt.Sprintf` into it: the specifics go in attributes, or
  grouping by `msg` in Loki stops working. (Restating the `msg` rule above, because this is
  where it gets broken.)
- **Before adding an `info` line, ask what multiplies it.** If the answer is request rate or
  notification volume, it is `debug`.

### Removing a log line safely

Demoting a hot-path record to `debug` is only safe if the signal it carried survives
somewhere queryable. Check, in order:

1. Is there a **durable event** for it? Much of the pipeline already emits one — `publishEvent`
   in dispatch, `emitEvent` in delivery — and those outlive any log retention.
2. Is there a **span**? If the operation is traced, the detail is in Tempo.
3. Is there a **counter**? If not, add one. Aggregate visibility is what makes the
   demotion safe, and it is usually the thing that was missing.

If none of the three holds, you are not quieting a log — you are deleting the only signal.
