---
id: 0004
title: Adopt an ownership manifest for the review-remediation batch, with auth owned by a unit rather than escalated
status: Accepted
affects:
  - .claude/ownership.json
  - docs/adr/**
source: docs/reviews/2026-07-27-architecture-review.md — revised remediation order, step 2; decisions taken 2026-07-29
---

# ADR 0004: Ownership manifest for the review-remediation batch

**Status:** Accepted (2026-07-29)  
**Date:** 2026-07-29  
**Author:** Daryl Robbins

> **Format note.** This record carries YAML frontmatter, which ADRs 0001–0003 do not. The
> `agent-team-guardrails` parser reads `id` / `status` / `affects` from frontmatter and
> treats a record without it as authorising nothing. The prose headings below follow the
> existing house style. That 0001–0003 authorise nothing is correct and harmless — they
> predate the manifest and govern no shared surface.

---

## Context

Step 2 of the revised remediation order in the
[2026-07-27 architecture review](../reviews/2026-07-27-architecture-review.md) is a batch of
ten small, independently verified fixes spanning Go, deployment configuration and
documentation: findings 4, 13, 17, 20, 26, 27, 29, 31, 34 and 49. The work is wide but
shallow and the file sets barely intersect — the shape that parallel agents suit.

Running agents in parallel over one repository needs a boundary that is mechanical rather
than advisory. `.claude/ownership.json` is that boundary: its presence arms the write gate,
and a unit may write only the globs it owns.

Three properties of this batch forced decisions the shipped defaults do not make well.

**Finding 20 edits `internal/auth/jwt.go`.** The plugin ships an escalation table routing
anything matching `**/auth/**` to a human under clause E3. Under that default the batch's
central work unit is denied at its first write.

**`.claude/**` is itself a shared surface.** Writing the manifest arms the gate that then
governs the manifest, so any later correction to it needs an Accepted record naming the path.

**`docs/adr/**` is also a shared surface, and the repository had no authorising record.**
This produced a genuine deadlock on first contact: writing the manifest sealed both the
manifest and the decision directory, so the ADR that would authorise either could not be
written. The cycle was broken by deleting the manifest, seeding this record, and re-creating
the manifest afterwards. This record therefore lists `docs/adr/**` under `affects:` so the
directory stays gated but is no longer sealed — ADR 0005 can be written normally.

That trap is not specific to Hermes. Any repository adopting the plugin's example manifest
without an existing frontmatter-bearing Accepted record will hit it, and Hermes hit it while
holding three ADRs, because none of them parse as authorising.

## Decision

Adopt `.claude/ownership.json` with three units — `unit-go-security`, `unit-deploy-config`,
`unit-docs` — holding disjoint globs, `policy.verify_command = "make verify"`, and shared
surfaces limited to `docs/adr/**`, `go.mod`, `go.sum`, `migrations/**`, `api/**` and
`.claude/**`.

Override the shipped escalation table in exactly one respect: **`**/auth/**` is not routed
to a human.** `unit-go-security` owns `internal/auth/**` outright. Migrations remain E3.

## Why

**On the auth override.** The alternative — leaving `**/auth/**` under E3 — was considered
and rejected. It would deny findings 4 and 20 at their first write and escalate each back to
the same person who had approved both in substance minutes earlier, converting a batch into
a series of round trips. The guardrail exists to make auth changes deliberate, not to make
them slow twice.

What replaces it is not nothing: one unit owns the entire auth surface exclusively, so no two
agents can race on it, and the permitted changes are scoped in advance to two named findings.

The cost of being wrong is real and worth stating plainly: an agent with a wrong idea can now
modify JWT validation with no human on the write itself. The mitigations are that
`internal/auth/` carries unit tests that run under `make verify`, and that the diff is
reviewed before it lands. **If either stops being true, reverse this.**

**On three units rather than two, or none.** Sequential execution was considered — ten small
edits across disjoint files may not repay their coordination cost, and the plugin's own
guidance warns that parallelism does not pay when the bottleneck is substrate nobody has
built yet. Rejected because the substrate exists and the file sets are genuinely disjoint.
The units divide along language and toolchain lines (Go, YAML/JSON, Markdown), which is also
how verification divides.

**On the verify command.** `make verify` is the project's own gate and needs no
infrastructure. Checked by reading the Makefile rather than assumed: target at `Makefile:35`,
described there as "Full local verification gate (no infra needed)".

**A gap in that gate — verified, and load-bearing.** Store tests are `//go:build integration`,
so `make verify` does not run them. Finding 4 changes `EnsureHermesSigningKey`, whose only
coverage is `internal/store/postgres/jwt_keys_test.go` — an integration test that currently
asserts the **bug** as correct: "Second call updates secret", expecting `secret-2` to win. A
green `make verify` is therefore *not* evidence for finding 4. `unit-go-security` must run
`make infra-up` and `make test-integration` for that finding specifically, and must invert
that assertion rather than delete it.

## Consequences

- `unit-go-security` may write `internal/auth/**`, `internal/store/postgres/auth.go`,
  `internal/store/postgres/auth_test.go`, `internal/store/postgres/jwt_keys_test.go` and
  `internal/email/**`.
- **`cmd/**` is owned by nobody.** Finding 4's fix must therefore not change the
  `EnsureHermesSigningKey` signature: its three callers (`cmd/admin/main.go:37`,
  `cmd/inbox/main.go:43`, `cmd/user/main.go:37`), the interface at
  `internal/store/interfaces.go:118`, the admin store interface at `internal/admin/server.go:72`
  and the mock at `internal/admin/testutil_test.go:363` all sit outside every boundary. The
  configured-secret-mismatch warning must therefore log from inside the store rather than
  return a value to callers.
- `unit-deploy-config` may write `deploy/**` and `charts/**`. `unit-docs` may write `docs/**`
  except `docs/adr/**`.
- A teammate spawned under any name other than an exact unit id owns nothing, and every write
  it attempts is denied. **What detects it:** the denial reaches the agent but not the lead,
  so it surfaces only in `/agent-team-guardrails:lint-manifest`, which reports unknown-identity
  and misattribution lines. Run that lint before trusting any completion report.
- Changing `.claude/ownership.json` again requires amending this record or writing another.
  That is deliberate friction on a file whose entire value is being frozen.
- `docs/reviews/**` is owned by `unit-docs` but is off-limits during fan-out by instruction,
  not by gate: the lead writes finding-resolution notes after the batch lands, so three agents
  cannot race on one file. **This is the weakest control in the manifest** — an instruction,
  not a boundary. If it is violated, the symptom is a conflicted or half-updated review doc.

## What I could not check

- ~~**Whether the harness binds a spawned agent's identity to a unit id.**~~ **RESOLVED
  2026-07-29 — it does not, and the fan-out failed on exactly this.** All three implementers
  were denied on their first write, including writes to files explicitly in their own `owns`
  list, with "this agent's identity did not resolve — no unit could be determined from the
  hook payload". Reproduced, not a flake. Two independent confirmations: `SendMessage`
  addressed to a unit id reports no such agent, and subagents were additionally unable to
  write *outside* the repository root, indicating the gate could not resolve their session
  root either. The lead's own writes succeed, so the gate resolves the lead and nothing else.
  **Net effect: while this manifest exists, no subagent can write anything at all.** The
  manifest is not merely inert for subagents — it is actively blocking, with no configuration
  available to fix it, because the Agent tool exposes no `name` parameter to bind.
- **Whether `make infra-up` succeeds here.** `docker info` exits 0, so the daemon is
  reachable, but the compose stack has not been started this session. Finding 4 cannot be
  verified without it.
- **Whether three concurrent agents running `make verify` in one working tree interfere.**
  The units edit different languages, and `verify-manifests` reads only YAML that
  `unit-deploy-config` alone writes, so the window is small — but it is a race, not an
  impossibility. If false failures appear, register worktrees in the manifest's `worktrees`
  map and re-run.
- **Whether the E3 override is the right long-term posture.** It is scoped to this batch. A
  future batch touching auth should re-derive it rather than inherit it.

## Status history

- 2026-07-29 — Accepted. Seeded by hand to break the decision-directory bootstrap cycle
  described in Context. The auth-escalation override is the substantive decision; the rest is
  scaffolding.
- 2026-07-29 — Amended: the mechanism this record adopts **never came into force.** Agent
  identity does not bind in this harness (see "What I could not check", first item), so the
  three units could not write and the batch was carried out by the lead directly. The record
  is kept rather than deleted because its reasoning is still live: the auth-escalation
  override, the `cmd/**` signature constraint on finding 4, and the finding that
  `make verify` does not cover finding 4's integration test all remain true and all still
  govern the work. Only the parallel-execution scaffolding is withdrawn.
  **Do not re-adopt an ownership manifest in this harness without first verifying identity
  binding with a single throwaway agent and one trivial write.** That check costs one agent
  round; skipping it cost three agents, a bootstrap deadlock, and a full fan-out.
