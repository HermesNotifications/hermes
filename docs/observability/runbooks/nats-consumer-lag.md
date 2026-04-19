# Runbook: `NATSConsumerLag`

## What this alert means

A NATS JetStream consumer has more than 1000 pending messages that haven't been delivered and acknowledged for at least 5 minutes. Either the worker is slow, the worker is down, or upstream is publishing faster than workers can consume.

## Immediate triage

```bash
# Which stream/consumer is lagging? The alert label shows it.
# kubectl port-forward the NATS service to get a local CLI:
kubectl -n hermes port-forward svc/nats 8222:8222 &
nats consumer info <stream> <consumer>
```

Dashboard: **Hermes infra** → "NATS consumer pending messages" panel.

## Common causes (ranked by frequency)

1. **Worker is down or scaled to zero.** Check the corresponding worker's `ServiceDown` status.
2. **Worker is slow.** Its HTTP-like throughput (messages/sec) has dropped. Often a downstream dependency is to blame — check the worker's error rate and latency.
3. **Upstream spike.** Legitimate traffic surge. Workers are healthy but not scaled enough.
4. **Poison message stalling the consumer.** Worker can't process message X, keeps retrying, nothing behind X moves. Check worker logs for repeated errors.
5. **NATS broker issue.** Rare but check `nats stream info` — is the stream reporting healthy? Disk full?

## Mitigations

### If worker down

See `service-down.md` for that worker service.

### If worker slow

- Check the worker's `HighLatency` / `HighErrorRate` alerts.
- Scale up replicas: `kubectl scale deployment hermes-worker-<kind> --replicas=<n>`.
- HPA should handle this automatically — if it isn't, investigate metrics-server.

### If upstream spike

- Confirm it's legitimate (not a retry storm). Check the publishing service's RPS.
- Temporary: increase worker replicas to the spike's shape.
- Long-term: either the HPA target needs tuning, or add a rate-limit upstream.

### If poison message

1. Get the message:
   ```bash
   nats stream view <stream> --raw
   ```
2. If safe to drop: `nats consumer next <stream> <consumer> --ack` to advance past it.
3. Open a bug — the worker needs to handle this input shape without getting stuck.

### If NATS itself

- Check `nats stream info <stream>` for `bytes` vs max.
- If full, either GC old messages or increase the stream storage.
- Escalate to infra on-call.

## Escalation

- Worker owner teams for their own workers.
- Infra for NATS-level issues (broker, storage).

## Post-incident

- If a poison message caused it, add a test for that input shape.
- If HPA didn't scale fast enough, tune target.
- If the stream hit disk limits, reassess retention / sizing.
