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
| [0005](0005-transport-security-for-infrastructure-connections.md) | Authenticate and encrypt connections to NATS, Postgres and Redis, with a config surface rather than connection-string archaeology | Accepted | 2026-07-29 |
| [0006](0006-migration-job-as-an-argocd-presync-hook.md) | Run the migration Job as an ArgoCD `PreSync` hook, and gate the sync on it | Accepted (amended 2026-07-31) | 2026-07-30 |
| [0007](0007-aws-network-and-control-plane-posture.md) | Fix the AWS network address plan and the EKS control-plane posture before either environment exists | Accepted | 2026-07-31 |
| [0008](0008-helm-chart-provisioning-jobs-are-not-hooks.md) | Run the Helm chart's provisioning Jobs as plain resources, not Helm hooks | Accepted | 2026-07-31 |
| [0009](0009-bundled-datastores-as-plain-manifests-on-official-images.md) | Ship the bundled evaluation Postgres and Redis as plain manifests on Docker Official Images | Accepted | 2026-07-31 |

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

1. Copy [`template.md`](template.md) to `NNNN-kebab-case-title.md`, where `NNNN` is the
   next zero-padded number (one above the highest in the index).
2. Fill in Context → Decision → Consequences → Alternatives considered. Be honest about
   costs; an ADR that lists only upsides is not trustworthy.
3. Add a row to the [Index](#index) table above.
4. Cross-link: reference the ADR from the relevant PR, and if the decision spawns
   follow-up work, open an issue and link it from the ADR (and back).
5. Open the PR with the ADR included. ADRs are reviewed like code.

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
