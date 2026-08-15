# Runbook: `HermesRoutingDropRate`

## What this alert means

More than half of routing decisions in the last 15 minutes selected no channel to deliver
on. The send succeeded, the API returned 202, a notification row exists — and nothing was
ever handed to a worker.

This is the quietest way for Hermes to break. Nothing fails, nothing retries, nothing
dead-letters, and no error rate moves. The only evidence is this counter and the absence
of deliveries.

## Drops are not errors

A steady background of drops is **normal and correct**. Every reason below is the routing
rules working as designed:

| `reason` | Means |
|---|---|
| `no_channels_for_template` | The template defines content for no channel the request asked for |
| `no_contact` | The user has no contact point for that channel (no email address, no phone) |
| `no_contact_for_any_channel` | The user has no contact point for any selected channel |

So the threshold is deliberately loose (50%, sustained 30 minutes) and the severity is
`warn`. What matters is a **step change**, not the level. Compare against the last week
before concluding anything:

```promql
sum by (reason) (rate(hermes_routing_drop_total[15m]))
```

## Triage

**Which reason moved?** That single breakdown usually names the cause.

- **`no_channels_for_template` jumped** → a template changed. Someone edited channel
  content, or a deploy shipped a template whose `content` map no longer covers the
  channels callers request. Check what changed in the templates table recently, and
  confirm against the caller's `channels` array — an explicit channel the template has no
  content for drops here.

- **`no_contact` / `no_contact_for_any_channel` jumped** → contact points stopped
  resolving. Either a user-data migration cleared them, or the caller changed what it
  sends: `to.contacts` overrides on the send request are the fallback when the stored
  user has no address, so a client that stopped sending them will drop everything for
  users who were relying on them.

- **Drops rose but so did traffic** → check the ratio, not the rate. A new high-volume
  caller sending to users who have never set a contact point looks exactly like a
  regression and is not one.

**Confirm against a specific notification.** Drops are logged at Debug with the
notification ID, and the routing decision is durable in the events table:

```
{k8s.namespace.name="hermes", k8s.container.name="dispatch"} | json | msg=~".*no contact.*"
```

## What to check in the code

Channel resolution is `internal/dispatch/channels.go`, and the precedence is documented
in [architecture.md](../../architecture.md#channel-resolution). The rule that surprises
people: a user preference is a **boolean opt-in gate, not a channel selection** —
`user_subscriptions` has no channel column. A category marked `required` skips the
preference check entirely.

Two narrowing passes run after channel selection, and both produce drops: channels the
template defines content for, then channels the recipient has a contact point for.

## Post-incident

- If a template shipped without content for a channel its callers use, that is a
  validation gap at template-write time, not a dispatch bug.
- Consider whether the affected category should be `required`, which bypasses the
  preference gate entirely.
