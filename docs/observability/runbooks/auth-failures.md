# Runbook: `HermesAuthFailureRate`

## What this alert means

More than half of authentication attempts against one service, on one scheme
(`api_key` or `jwt`), were rejected over 10 minutes.

The alert is deliberately loose. It is not a rate limiter — `internal/middleware`
already sheds volume, and `RateLimitBackendDegraded` covers that machinery. This exists
to catch the two things a 401 rate cannot distinguish on its own.

## The two readings

**Operational: a credential stopped working.** A deploy rotates a key, a caller does not
pick up the new one, and from their side Hermes is completely down. From our side every
health check is green, every pod is Ready, and the HTTP error-rate alert does not fire
because 401 is not 5xx. This alert is the only one that sees it.

**Security: someone is guessing.** A credential-stuffing run is a rising `invalid_key`
rate against a flat `ok` rate. Nothing else in Hermes can see it — the requests are
well-formed, served fast, and never reach a handler.

The breakdown tells you which:

```promql
sum by (service, scheme, reason) (rate(hermes_auth_result_total[10m]))
```

| `reason` | Reading |
|---|---|
| `missing_credential` | No `Authorization` header at all. A misconfigured client, a health check pointed at the wrong path, or a scanner. |
| `invalid_key` | API key present and rejected. Rotation, or guessing. |
| `invalid_token` | JWT present and did not verify against any configured key. Expiry, or a key rotated out. |
| `missing_claims` | Token verified but carries no usable `sub` / organization claim. A token-issuing bug, not a caller problem. |
| `no_signing_keys` | **Ours.** The service has no JWT signing keys configured and is refusing everything with a 500. |

## Triage by reason

**`no_signing_keys`** → stop here, this is a Hermes misconfiguration. The service is
rejecting every authenticated request. Check the JWT secret env/secret mount on the
affected deployment; it is almost always a missing or misnamed Kubernetes secret after a
deploy.

**`invalid_key` or `invalid_token`, correlated with a deploy** → credential rotation. The
timestamp is the giveaway: the failure rate steps at the rollout. Confirm which key
version the caller is using, and check whether the old key was removed before the caller
migrated. Hermes supports multiple JWT signing keys precisely so rotation can overlap —
if a rotation removed the old key immediately, re-adding it buys time to migrate.

**`invalid_token` with no deploy behind it** → most often mass token expiry, if a batch of
tokens was issued with the same TTL. Check the issuing path before assuming an attack.

**`invalid_key` climbing against a flat `ok` rate, no deploy** → treat as an attack.
The counter deliberately carries no key or organization identifier, because an attacker
controls those values on a failing request and labelling by them would be an unbounded
label anyone on the internet could inflate. Attribution is on the span and in the access
logs:

```
{k8s.namespace.name="hermes"} | json | msg="request" | status=401
```

Source IPs come from the ingress logs. If it is a single source, block at the ingress —
the IP rate limiter runs outside auth for exactly this reason, but its default limits are
sized for accidental floods, not deliberate ones.

## What this alert does not cover

Authorization. A caller that authenticates successfully and is then refused for lacking a
permission does not appear here — `auth.CheckPermission` fails closed and returns 403,
and those are visible only as 4xx in the HTTP metrics. That is a separate gap.

## Post-incident

- Rotation that breaks a caller means the overlap window was too short. Both schemes
  support multiple valid keys; use them.
- Persistent `missing_credential` from one source is usually a monitoring check or
  scanner pointed at an authenticated path. `/healthz` and `/readyz` skip auth and are
  the correct targets.
