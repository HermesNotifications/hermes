# Runbook: `HermesDeadLetterDetected` / `HermesDLQPublishFailure`

## What these alerts mean

**`HermesDeadLetterDetected` (warn):** a consumer gave up on a message — it either
failed all delivery attempts (`reason=max_deliveries`) or was rejected as permanently
unprocessable (`reason=terminated`). The message is preserved on the `DLQ` stream
(subject `dlq.<original subject>`) for 7 days / up to 1 GiB. Any dead letter is
abnormal and worth a look.

**`HermesDLQPublishFailure` (critical):** a terminal failure could NOT be captured —
the DLQ publish itself failed. Originals are left unacked in their source stream
(retained until the 7-day MaxAge) rather than destroyed, but the safety net is
failing. This almost always means NATS itself is unhealthy (disk full, cluster
degraded) — treat it as a NATS incident first.

## Immediate triage

The alert labels show `stream`, `consumer`, and (for dead letters) `reason`.

```bash
kubectl -n hermes port-forward svc/nats 4222:4222 &

# How many dead letters, and on which subjects?
nats stream info DLQ
```

- `reason=max_deliveries` → the handler kept failing. Usually a downstream outage
  (SMTP relay, SMS webhook, Centrifugo, database) that outlasted the ~10-minute
  retry window. Check the consumer's error rate and the downstream dependency.
- `reason=terminated` → the handler rejected the message as unprocessable
  (malformed payload, invalid tenant UUID). This is a bug or bad input upstream;
  replaying without a fix will land it straight back on the DLQ.

## Inspect dead letters

```bash
# Read dead letters without consuming them (DLQ uses limits retention —
# reads never delete):
nats consumer add DLQ inspect --pull --filter 'dlq.>' --ack explicit --deliver all
nats consumer next DLQ inspect --count 10
```

Each message is a JSON envelope:

```json
{
  "subject": "delivery.email",      // original subject — republish here to replay
  "stream": "DELIVERY",
  "consumer": "worker-email",
  "reason": "max_deliveries",
  "attempts": 10,
  "error": "smtp: connection refused",
  "failed_at": "2026-06-12T10:00:00Z",
  "payload": { ... }                // original message body, verbatim
}
```

Clean up the inspection consumer when done: `nats consumer rm DLQ inspect -f`.

## Replay

1. Fix the underlying cause first (downstream recovered, bug fixed and deployed).
2. Republish the envelope's `payload` to its original `subject`:

   ```bash
   # Single message — paste the payload object only, not the whole envelope:
   nats pub delivery.email '{"notification_id":"...", ...}'
   ```

3. Replay is safe to repeat: every pipeline stage is idempotent (the Send service
   dedups on the idempotency key in Redis, dispatch dedups notification creation
   in the DB, and the event writer uses conditional rank-based writes). A duplicate
   replay is a no-op, not a duplicate notification.
4. For `reason=terminated` messages: do NOT replay until the defect that made the
   payload unprocessable is fixed — they will terminate again on first delivery.

## Purge after resolution

```bash
# Remove handled dead letters for one subject:
nats stream purge DLQ --subject dlq.delivery.email -f

# Or everything:
nats stream purge DLQ -f
```

## Post-incident

- `max_deliveries` from a downstream outage: consider whether the outage should
  page on its own (e.g. SMTP relay health) before messages exhaust retries.
- `terminated`: add a test for the input shape that caused it; fix the producer
  if it published a malformed message.
- `HermesDLQPublishFailure`: review the NATS incident; confirm the stranded
  source-stream messages were replayed or aged out as intended.
