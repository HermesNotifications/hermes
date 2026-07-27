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
