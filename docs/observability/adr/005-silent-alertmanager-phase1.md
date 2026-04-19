# ADR-005: Alertmanager deployed, routing silent in Phase 1

**Date:** 2026-04-19
**Status:** Accepted

## Context

Phase 1 stands up Prometheus + Alertmanager in production. The question is: do we wire alert destinations (Slack, PagerDuty, email) immediately, or defer?

## Decision

**Rules codified, routing deferred.** Alertmanager runs with a single `null-receiver`. Alerts fire, enter the UI, are visible — but no human is paged.

## Consequences

### Why rules now, destinations later

- **Thresholds are a guess until measured.** Any threshold we pick before seeing real production load will be wrong. Wiring Slack Day 1 guarantees alert fatigue.
- **Rules are cheap to codify; hard to un-adopt.** Writing the rule now captures what we *think* matters. Letting them run silent for a month gives real data on false-positive rates.
- **Testing the alert evaluation pipeline is still valuable.** Even silent, we want to confirm: rules evaluate correctly, query syntax is valid, metrics/labels referenced actually exist. A silent-firing alert verifies the full pipeline short of the destination.

### What "silent" actually means

- Alertmanager's `route.receiver: null-receiver` matches every alert.
- `null-receiver` has no integrations; Alertmanager acknowledges and drops.
- The Alertmanager UI still shows all firing/resolved alerts, grouped, searchable.
- Every firing alert is visible on the `observability-health` dashboard.

### Phase 2 wiring

When moving from silent → live:

1. Pick one alert to wire first (the most obviously paging-worthy, probably `ServiceDown`).
2. Replace `null-receiver` for that rule's route with a Slack receiver.
3. Observe for two weeks. Tune threshold. Ship runbook updates.
4. Promote to PagerDuty only when Slack signal is reliable.
5. Repeat per rule.

Don't wire all rules at once even at Phase 2. **Alertmanager routing complexity is the #2 source of on-call pain after bad thresholds.**

### What this doesn't mean

- **Not** "alerts don't matter in Phase 1." Firing alerts still indicate problems; the `observability-health` dashboard is a legitimate place to look.
- **Not** "we'll skip the runbooks." Runbooks ship with rules in the same PR. When routing activates, the runbook is already there.
