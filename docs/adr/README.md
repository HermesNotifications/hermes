# Architecture Decision Records

An **Architecture Decision Record (ADR)** captures a significant architectural decision,
its context, and its consequences — so a future maintainer can understand *why* the system
is the way it is without reverse-engineering it from the code.

This directory follows the [MADR](https://adr.github.io/madr/) / Michael Nygard convention.

## Index

| # | Title | Status | Date |
|---|---|---|---|
| [0001](0001-dynamodb-model-via-extenddb.md) | Adopt the DynamoDB programming model for the hot notification path | Accepted | 2026-05-29 |
| [0002](0002-provider-plugin-model-bus-native-isolation.md) | Adopt a provider plugin model with bus-native (NATS subject) isolation | Accepted | 2026-06-13 |
| [0003](0003-rename-tenant-to-organization.md) | Rename `tenant` to `organization`, and name the app as the isolation boundary | Accepted | 2026-07-27 |
| [0004](0004-ownership-manifest-for-review-remediation.md) | Ownership manifest for the review-remediation batch, with auth owned by a unit rather than escalated | Accepted | 2026-07-29 |
| [0005](0005-transport-security-for-infrastructure-connections.md) | Authenticate and encrypt connections to NATS, Postgres and Redis, with a config surface rather than connection-string archaeology | Accepted (amended 2026-08-12) | 2026-07-29 |
| [0006](0006-migration-job-as-an-argocd-presync-hook.md) | Run the migration Job as an ArgoCD `PreSync` hook, and gate the sync on it | Accepted (amended 2026-07-31) | 2026-07-30 |
| [0007](0007-aws-network-and-control-plane-posture.md) | Fix the AWS network address plan and the EKS control-plane posture before either environment exists | Accepted | 2026-07-31 |
| [0008](0008-helm-chart-provisioning-jobs-are-not-hooks.md) | Run the Helm chart's provisioning Jobs as plain resources, not Helm hooks | Accepted | 2026-07-31 |
| [0009](0009-bundled-datastores-as-plain-manifests-on-official-images.md) | Ship the bundled evaluation Postgres and Redis as plain manifests on Docker Official Images | Accepted | 2026-07-31 |
| [0010](0010-bounded-work-streams-reject-rather-than-drop.md) | Bound the JetStream work streams and reject new work when they fill | Accepted | 2026-08-10 |
| [0011](0011-api-keys-are-scoped-to-an-organization.md) | Scope API keys to an organization, and derive the organization from the key | Superseded by [0012](0012-api-keys-are-not-scoped-to-organizations.md) | 2026-08-10 |
| [0012](0012-api-keys-are-not-scoped-to-organizations.md) | API keys are not scoped to organizations; the organization is a per-request parameter | Accepted | 2026-08-11 |
| [0013](0013-embeddable-inbox-widget-contract.md) | Ship one inbox implementation as a custom element with a versioned public contract, wrapped rather than reimplemented for React | Accepted (amended 2026-08-11) | 2026-07-30 |
| [0014](0014-cache-first-unread-count.md) | Serve the unread count from cache, and keep it off the websocket's critical path | Accepted | 2026-08-10 |
| [0015](0015-lifecycle-and-jetstream-durability.md) | Drain before shutdown, and replicate the streams the cluster can lose | Accepted | 2026-08-10 |
| [0016](0016-distributed-rate-limiting-with-local-fallback.md) | Rate limit per credential in Redis, with the local bucket as the fallback | Accepted | 2026-08-11 |
| [0017](0017-realtime-transport-ladder.md) | Fall back from WebSocket to HTTP-streaming then SSE, derived from one URL | Accepted | 2026-08-11 |
| [0018](0018-client-lifecycle-dispose-is-terminal.md) | `dispose()` is terminal, `disconnect()` is the reusable one, and the store repairs its own wiring | Accepted (amended 2026-08-12) | 2026-08-12 |
| [0019](0019-notification-metadata-passthrough.md) | Carry an opaque metadata object end to end, and reserve exactly two keys in it | Accepted | 2026-08-12 |
| [0020](0020-project-identity-and-registry.md) | Own one spelling of the project's name, under the `HermesNotifications` org | Accepted | 2026-08-12 |
| [0021](0021-bootstrap-the-first-api-key-into-a-secret.md) | Create the first API key at install time and put it in a Secret | Accepted | 2026-08-12 |
| [0022](0022-liveness-follows-consumer-progress.md) | Fail liveness when a NATS consumer holds work and settles none of it | Accepted | 2026-08-13 |

> Keep this table in sync whenever you add or change an ADR's status.

## When to write one

Write (or update) an ADR **in the same PR as the change** when a decision:

- introduces or replaces a **datastore, messaging backbone, auth model, or cross-service
  contract**;
- is **costly to reverse**, or one a future maintainer would otherwise have to
  reverse-engineer from the code;
- selects between **competing approaches** where the rejected options are worth recording.

You do *not* need an ADR for routine, easily-reversible, or purely-local choices — those
belong in code comments or PR descriptions. When in doubt, prefer writing one: a short ADR
is cheap, and a missing one is expensive.

## How to write one

1. Scaffold it, rather than picking a number by hand:

   ```bash
   make adr-new TITLE="Bound the JetStream work streams"
   ```

   This copies [`template.md`](template.md) and fills in the number, date and author. See
   [Numbering](#numbering) for why the number is not simply "one above the highest in the
   index" — that instruction lived here for months and is what caused the collisions.
2. Fill in Context → Decision → Consequences → Alternatives considered. Be honest about
   costs; an ADR that lists only upsides is not trustworthy.
3. Add a row to the [Index](#index) table above. `make verify` fails if you forget.
4. Cross-link: reference the ADR from the relevant PR, and if the decision spawns
   follow-up work, open an issue and link it from the ADR (and back).
5. Open the PR with the ADR included. ADRs are reviewed like code.

## Numbering

An ADR number is a global identifier handed out at authoring time by people who cannot see each
other. Your `docs/adr/` is a snapshot of the `main` you branched from: it cannot show you the
ADR that landed there yesterday, nor the one sitting in an open PR that will land before yours.
So "one above the highest" is right on every branch and wrong in aggregate, and nothing notices
— each branch renders, reviews and merges cleanly, because the collision only exists relative to
a `main` that neither has met yet.

It has already happened. PR #73 carried an 0010 while `main` independently landed a different
0010; a branch stacked on #73 then added an 0011 and an 0012 against `main`'s own 0011 and 0012.
Three collisions, found by reading rather than by any tool.

Two things now cover it:

- **`make adr-new`** allocates against your tree, `origin/main`, *and* the ADRs in open pull
  requests — the third being the one the other two cannot see. It warns and continues when it
  cannot reach the network, so it never blocks you offline.
- **`scripts/check_adr_numbering.py`**, run by `make verify` and in CI, fails when a number
  means one thing here and another on `origin/main`, when two files claim one number, when a
  heading disagrees with its filename, or when the index and the files drift apart. Run
  `make adr-next` to see the next free number at any time.

Neither is a lock — two people scaffolding in the same minute still race. The check is the
backstop, and it runs on every PR.

**Numbers may have gaps.** A withdrawn ADR leaves a hole, and renumbering the survivors to close
it would break every reference to them. The check permits gaps deliberately.

**If you do collide,** renumber *your* ADR rather than the one already on `main`: `git mv` the
file, update its `# ADR NNNN:` heading, update its row in the index, and fix references
(`grep -rn 'ADR 00NN' docs/ internal/ charts/ deploy/`). The check prints the next free number.

## Status lifecycle

```
Proposed → Accepted → Deprecated
                    ↘ Superseded by NNNN
```

- **Proposed** — under discussion, not yet adopted.
- **Accepted** — the decision is in force.
- **Deprecated** — no longer recommended, with no direct replacement.
- **Superseded by NNNN** — replaced by a later ADR.

## Amend vs. supersede

ADRs are an **append-only log of what was decided, and when** — you don't rewrite history.

- **Clarifications and corrections** (tightening wording, fixing an inaccuracy, recording
  a minor follow-up) — **amend the ADR in place**, and note it in the `Status` line with a
  date, e.g. `Accepted (amended 2026-06-12: …)`.
- **Substantive reversals** (changing or undoing the decision itself) — **do not edit the
  old ADR's decision.** Write a **new ADR** that `Supersedes NNNN`, and set the old one's
  status to `Superseded by <new#>`. This keeps the original reasoning intact and shows the
  evolution.

When a decision is deliberately deferred-but-not-cancelled, record the **revisit trigger**
(the concrete conditions under which it will be re-evaluated) inside the ADR, and open a
tracking issue so it isn't forgotten.
