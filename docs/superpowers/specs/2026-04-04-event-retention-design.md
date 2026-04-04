# Notification Event Retention

## Context

The `notification_events` table grows unbounded — events are inserted continuously by the event-writer service but never deleted. With no retention policy, this table will dominate storage and degrade vacuum performance over time. We need a configurable retention period (default 90 days) and an automated cleanup mechanism.

## Design

### Approach: Batched DELETE via standalone cleanup command

A new `cmd/cleanup` binary performs batched deletes of events older than the configured retention period. It runs as a Kubernetes CronJob (daily), avoiding coordination issues with multiple event-writer instances. No partitioning — the table stays as-is since queries are by `notification_id` (not time ranges) and 90 days of data is bounded enough that DELETE + vacuum handles it well.

### 1. Migration (`000014_drop_events_fk.up.sql`)

Drop the foreign key constraint on `notification_events.notification_id`. This constraint adds write overhead on every insert (FK check) and would block deletes if the parent notification still exists. Referential integrity is already enforced at the application layer — event-writer only processes events for existing notifications.

```sql
-- up
ALTER TABLE notification_events DROP CONSTRAINT notification_events_notification_id_fkey;

-- down
ALTER TABLE notification_events
  ADD CONSTRAINT notification_events_notification_id_fkey
  FOREIGN KEY (notification_id) REFERENCES notifications(id);
```

### 2. Configuration

Add `EventRetentionDays` to `internal/config/config.go`:

```go
EventRetentionDays int  // HERMES_EVENT_RETENTION_DAYS, default 90
```

The cleanup command reads this alongside `DatabaseURL`. No other services need this config value.

### 3. Store method

Add to `EventRepository` interface in `internal/store/interfaces.go`:

```go
DeleteEventsOlderThan(ctx context.Context, before time.Time, batchSize int) (int64, error)
```

Implementation in `internal/store/postgres/events.go`:

```sql
DELETE FROM notification_events
WHERE id IN (
    SELECT id FROM notification_events
    WHERE created_at < $1
    LIMIT $2
)
```

The subquery with `LIMIT` bounds lock acquisition per batch. Returns the number of rows deleted so the caller can loop until 0.

### 4. Cleanup command (`cmd/cleanup/main.go`)

Standalone binary, follows the same pattern as `cmd/migrate`:

- Reads `HERMES_DATABASE_URL` and `HERMES_EVENT_RETENTION_DAYS` from env (flags with env fallback)
- Connects to Postgres, creates store
- Loops: calls `DeleteEventsOlderThan(now - retentionDays, 5000)` until 0 rows deleted
- Logs total rows deleted and duration
- Exits

### 5. Makefile

Add to the `SERVICES` list and add a convenience target:

```makefile
SERVICES := admin send dispatch worker-events worker-email worker-sms worker-inbox inbox user migrate seed cleanup

cleanup:           ## Run event retention cleanup
	go run ./cmd/cleanup/ -database-url "$(DB_URL)"
```

### 6. Kubernetes CronJob

Add a K8s CronJob manifest (daily at 3:00 AM UTC). Follows existing deployment patterns. Uses the same Docker image build pattern as other services (`make docker-cleanup`).

## Files to modify

| File | Change |
|------|--------|
| `migrations/000014_drop_events_fk.up.sql` | New: drop FK constraint |
| `migrations/000014_drop_events_fk.down.sql` | New: restore FK constraint |
| `internal/config/config.go` | Add `EventRetentionDays` field |
| `internal/store/interfaces.go` | Add `DeleteEventsOlderThan` to `EventRepository` |
| `internal/store/postgres/events.go` | Implement batched delete |
| `cmd/cleanup/main.go` | New: standalone cleanup binary |
| `Makefile` | Add `cleanup` to SERVICES, add `cleanup` target |
| `deploy/k8s/base/cleanup-cronjob.yaml` | New: daily cleanup CronJob (alongside existing `migration-job.yaml`) |
| `deploy/k8s/base/kustomization.yaml` | Add cleanup-cronjob.yaml to resources |

## What stays unchanged

- Event insert path (batch insert in event-writer)
- `GetNotificationEvents` query
- Status rollup (`BatchUpdateNotificationStatuses`)
- All other store interfaces and service code

## Verification

1. `make test` — unit tests pass (add test for `DeleteEventsOlderThan`)
2. `make test-integration` — integration test: insert events with old timestamps, run cleanup, verify only recent events remain
3. `make migrate` then `make cleanup` against local dev — verify it runs and logs correctly
4. Verify FK is dropped: `\d notification_events` in psql shows no FK constraint
