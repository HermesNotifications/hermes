---
name: writing-adrs
description: Use when making, recording, amending, or reversing an architecturally significant decision in this repo — introducing/replacing a datastore, messaging backbone, auth model, or cross-service contract, or any choice costly to reverse. Also use when the user asks to "write an ADR", "record this decision", or "supersede an ADR". Guides creating/updating an Architecture Decision Record under docs/adr/.
---

# Writing Architecture Decision Records

`docs/adr/README.md` and `docs/adr/template.md` are the **source of truth** for the
conventions and structure. This skill is the procedure; read those files for the detail.

## 1. Decide whether an ADR is warranted

Write one when the decision introduces/replaces a **datastore, messaging backbone, auth
model, or cross-service contract**, is **costly to reverse**, or **chooses between
approaches** worth recording. Skip it for routine, local, easily-reversed choices. When in
doubt, write a short one.

## 2. Decide: new ADR, amend, or supersede

- **New decision** → new ADR (continue below).
- **Clarification / correction** to an existing ADR → amend it in place; add a dated note
  to its `Status` line. Do **not** create a new ADR.
- **Reversing or changing** an existing decision → write a **new** ADR that supersedes the
  old one. Do **not** edit the old ADR's decision. Set the old one's status to
  `Superseded by <new#>` and reference it from the new ADR's Context.

## 3. Create the ADR

1. Read `docs/adr/README.md` to find the highest existing number; the new file is
   `docs/adr/NNNN-kebab-case-title.md` (next zero-padded number).
2. Copy `docs/adr/template.md` into it and fill in: Context → Decision → Consequences →
   Alternatives considered. Add optional sections (Scope/Rollout, Tradeoffs, design) only
   if they earn their place.
3. Be honest about costs and rejected options — that is the most valuable part.
4. If the decision is deferred-but-not-cancelled, record a concrete **revisit trigger** in
   the ADR and open a tracking issue.

## 4. Wire it up (same PR as the change)

- [ ] Add/Update the row in the Index table in `docs/adr/README.md`.
- [ ] If superseding: flip the old ADR's `Status` to `Superseded by NNNN`.
- [ ] Cross-link the ADR from the PR description, and link any tracking issue from the ADR
      (and back).
- [ ] Ship the ADR **in the same PR** as the change it describes. ADRs are reviewed like
      code.

## Checklist summary

Recognize → choose new/amend/supersede → write from template → update index → cross-link →
ship in the same PR.
