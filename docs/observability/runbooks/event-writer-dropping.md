# Runbook: `HermesEventWriterDropping`

## What this alert means

A batch write in the event writer failed and its events were discarded.

**This is permanent data loss.** It is not a retry that will succeed later. NATS messages
are acked when they enter the in-memory batcher, not when the database write completes —
so once a flush fails, nothing exists to redeliver. The events are gone.

That is why this pages at any nonzero value rather than warning above a threshold.

## The two stages

The `stage` label says what was lost:

**`stage=insert`** — `InsertEvents` failed. The event rows never landed. The audit trail
for those notifications has a hole in it, and because status updates are derived from the
same batch, statuses did not advance either.

**`stage=status`** — the events were written but `BatchUpdateNotificationStatuses` failed.
The history is intact; the rollup is stale. Users see this directly: a notification that
was delivered still reads as `pending` or `sent` in the inbox, indefinitely, because the
status ladder only ever advances on an event that has already been consumed.

## Immediate triage

This is nearly always Postgres. Check it first, before anything in Hermes:

```promql
# Pool exhaustion is the most common cause and has its own alert.
hermes_db_pool_connections / hermes_db_pool_max_connections
rate(hermes_db_pool_acquire_waits_total[5m])
```

```
{k8s.namespace.name="hermes", k8s.container.name="worker-events"} | json | level="ERROR"
```

The log line carries the driver error and the batch size. Look for:

- **connection refused / too many connections** → Postgres is down or out of connection
  slots. See [db-pool-saturated.md](db-pool-saturated.md).
- **statement timeout** → the batch insert is exceeding the statement timeout, usually
  because the events table has grown without its indexes or autovacuum is behind.
- **constraint violation** → a defect, not an outage. A malformed event is failing the
  whole batch, which means one bad message is destroying up to 99 good ones alongside it.
  Get the payload from the log and file it.
- **disk full** → see [disk-pressure.md](disk-pressure.md).

## Assessing the damage

Events lost is the alert value; the notifications affected are harder to bound, because
the dropped batch is exactly the record of which ones they were. Two approximations:

```sql
-- Notifications stuck below their true status. For stage=status drops these are
-- recoverable: the events survived, so the rollup can be recomputed.
SELECT n.id, n.status, max(e.event), max(e.created_at)
FROM notifications n
JOIN notification_events e ON e.notification_id = n.id
WHERE n.created_at > now() - interval '1 hour'
GROUP BY n.id, n.status
HAVING max(e.event) LIKE '%.sent' AND n.status IN ('pending', 'sent');
```

For `stage=status`, replaying the rollup from the surviving events restores correctness.
For `stage=insert`, the events themselves are gone and there is nothing to replay from —
the delivery still happened, and the DLQ does not hold these (they were acked, not
failed).

## Recovery

1. Fix the database problem. Nothing else matters until flushes succeed —
   `rate(hermes_eventwriter_events_total[5m])` returning to its normal level is the
   confirmation.
2. For `stage=status`, recompute the rollup for the affected window from
   `notification_events`. The update is conditional on rank, so re-running it is safe and
   idempotent — status never regresses.
3. For `stage=insert`, accept the loss and note the window in the incident record.

## Post-incident

The acking model is the root cause and is worth revisiting: acking on entry to the
batcher is what makes the batching fast and what makes a flush failure unrecoverable.
Acking after a successful flush would trade throughput for durability here.

If the cause was one malformed event failing a whole batch, per-row error handling on the
insert would contain the blast radius from 100 events to 1.
