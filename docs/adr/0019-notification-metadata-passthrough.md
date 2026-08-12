---
id: 0019
title: Carry an opaque metadata object end to end, and reserve exactly two keys in it
status: Accepted
affects:
  - internal/send/**
  - internal/nats/**
  - internal/dispatch/**
  - internal/delivery/**
  - internal/models/**
  - internal/store/**
  - migrations/**
  - api/**
  - sdks/typescript/packages/**
source: inbox widget polish pass, 2026-08-11
---

# ADR 0019: Notification metadata, with two reserved keys

**Status:** Accepted (2026-08-12)  
**Date:** 2026-08-12  
**Author:** Daryl Robbins

---

## Context

A notification could not say anything about itself beyond its text. There was no severity, no
importance, and no way for a sender to attach its own identifiers — and therefore no way for a
client to decide that one notification deserves to interrupt the user while another can wait in the
panel. Every integrator wanting a toast had to invent a private convention, usually by pattern
matching on the title.

There was also nowhere to put such a thing. `notifications` had no free-form column; `models.Notification`
had no free-form field; and the send API's `data` is template render input that is never persisted
and never reaches a client, so it looks like the seam and is not one.

Two constraints shaped the design more than anything else:

- **The inbox worker has no database.** `cmd/worker-inbox` wires NATS, Redis and Centrifugo and
  nothing else. Anything a client is to receive on `notification.new` must ride the message; it
  cannot be looked up at publish time. This is the same constraint that made `unread_count` optional
  in [ADR 0014](0014-cache-first-unread-count.md).
- **`SendMessage.Metadata` was already taken**, by `MessageMetadata{Template}` — Hermes's own
  routing information, unrelated to anything a sender would attach.

## Decision

**We will carry an opaque `metadata` object from the send request to the client, and interpret
exactly two keys in it.**

1. **`metadata` is a free-form JSON object**, stored verbatim and echoed back on the inbox row
   (`GET /v1/inbox`) and on the `notification.new` event. Persisted in a new nullable
   `notifications.metadata JSONB` column (migration `000020`).

2. **Hermes reads two reserved keys and will never reserve a third.**
   - `level` — `info | success | warning | error`, optional. How a client should present it.
   - `toast` — boolean, optional. Whether a client should surface it transiently.

   The commitment matters: reserving another bare top-level name later would break any caller
   already using it for their own data. A caller may use every other key freely.

3. **`level`, not `severity`.** `NotificationEvent.Severity` already exists with values
   `info`/`warn`/`error` for the operational delivery log. Two `Severity` fields in one package,
   with overlapping but unequal value sets, would be read as one concept and conflated.

4. **`level` and `toast` are independent.** An `error` that must not interrupt, and an `info` that
   should, are both real. Collapsing them into one field would make presentation and interruption
   the same decision.

5. **`level` is enum-validated at the send edge and an unrecognised value is rejected (422)** — not
   coerced, not dropped. `models.NotificationMetadata` implements `huma.SchemaProvider`, so the enum
   is declared once and enforced by huma before the handler runs, appears in both OpenAPI specs, and
   generates a TypeScript literal union rather than `unknown`.

6. **Clients treat an unrecognised `level` as no level.** The server may add levels; a client that
   predates one must stay renderable.

7. **The object is capped at 4 KiB serialized**, enforced in the send handler and not configurable.

8. **On the bus the field is `client_metadata`**, a sibling of the existing `metadata`, on both
   `SendMessage` and `DeliveryMessage`.

9. **Both stores carry it.** Postgres as `jsonb`; DynamoDB as a **JSON string**, not a native `M`,
   so both go through one `encoding/json` path.

10. **Toasts are the host's job, not the widget's.** `@hermes-notifications/web` gains no toast
    machinery. React gets `useHermesToasts` plus a provider-agnostic adapter interface, with a
    Sonner adapter on a separate subpath export and `sonner` as an *optional* peer dependency.

## Consequences

**Good.** A sender can express severity and urgency without inventing a convention, and can attach
its own identifiers to a notification for the first time. The widget renders `level` as a rail on
the row, so severity survives past the few seconds a toast is on screen. The React toast layer adds
no runtime dependency to anyone who does not opt in.

**"Verbatim" is a semantic round trip, not a byte-for-byte one**, and the docs say so. `jsonb` does
not preserve key order, strips insignificant whitespace, and collapses duplicate keys to the last.
`map[string]any` decodes every number to `float64`, so an integer above 2^53 loses precision. If
exact fidelity is ever needed, `map[string]json.RawMessage` is a drop-in with an identical emitted
schema.

**Adding a level later is safe; removing one is not.** Adding is a server-side relaxation that old
clients degrade past. Removing breaks callers who send it.

**An idempotent replay keeps the first request's metadata.** `POST /v1/send` returns the original
notification id for a repeated idempotency key, so a replay carrying different metadata is ignored.
This matches the existing behaviour for `content`, but it is more visible now.

**Costs.** A new column on the largest table (nullable, no default, so a catalog-only change). Six
regenerated spec files and four regenerated SDKs. A new public field on two REST schemas and one
websocket event, all additive. `internal/models` now imports huma, which links it into the three
worker binaries that did not have it — compile-time only.

**Follow-up, not done here.** `MessageMetadata` is misnamed and its AsyncAPI schema was stale (it
documented `group`/`type` long after the Go struct had only `template`); the schema is fixed here,
the name is not. Renaming it to `template` is wanted but is not a safe single step: during a rolling
deploy an old Send's `{"metadata":{"template":"welcome"}}` would be decoded by a new Dispatch as
user metadata, find no template, and silently degrade a template send into an empty direct-content
one — on a WorkQueue stream with retained messages, that is silent corruption rather than a crash.
It needs a dual-write/dual-read transition. Worth knowing: `DeliveryMessage.Metadata` currently has
no reader at all, so the rename is cheaper than it looks.

## Alternatives considered

**Discrete `level` and `toast` columns and API fields.** Simpler to validate and index, and the
first design. Rejected because every future addition costs another migration and another pair of
schema changes, and because the passthrough is independently valuable — the API had no escape hatch
at all, and integrators were asking the title to carry their identifiers.

**Nesting the reserved keys under `metadata.hermes.{level,toast}`.** Future-proof: Hermes could
reserve anything under its own namespace without ever colliding. Rejected as too verbose for the
common case, which is a caller setting a level and nothing else. The flat form plus an explicit
promise never to reserve a third key buys the same safety at a much better ergonomic.

**Reusing `data`.** It is already on the send request and already a `map[string]any`. Rejected: it
is template render input, deliberately never persisted and never sent to a client, and overloading
it would mean template variables leaking into every inbox response.

**Extending `MessageMetadata` with a nested user object.** Purely additive and wire-safe, and it
would have propagated to the fan-out for free since dispatch copies that field wholesale. Rejected
because it conflates two unrelated concepts under one word and makes `metadata.client.level` the bus
path for something the API calls `metadata.level`.

**Renaming `MessageMetadata` to `template` in the same change.** The clean end state, and cheap in
Go — three read sites and one dead write. Rejected as not wire-safe during a rolling deploy; see
Consequences.

**`json.RawMessage` for the model field.** Preserves numbers exactly and avoids a decode. Rejected
on the generated types: huma emits a bare empty schema for it, which openapi-typescript renders as
`unknown`, on which reading `.level` is a compile error. The typed union is most of the value.

**Validating `level` in dispatch rather than at the send edge.** Rejected: everything past
`notification.send` is asynchronous, so a bad value discovered there can only be logged or
dead-lettered and the caller never learns. The send handler is the only place with a synchronous
channel back to them.

**Coercing an unrecognised `level` to `info`, or dropping the key.** Both silent, both durable, and
both invent or discard intent the caller expressed. Rejected: `level` is optional, so rejecting only
affects a caller who typed something wrong, and they are the one who can fix it. Publishing an enum
in the schema while not enforcing it would be worse than publishing none — clients write exhaustive
switches over four values and meet a fifth in production.

**A `<HermesToaster/>` component in `@hermes-notifications/react` depending on sonner directly.**
One tag, zero config. Rejected: every consumer of the React package would carry sonner whether or
not they use it, and swapping would mean not using the component.

**Toasts rendered by the custom element.** Rejected on three grounds. A toast is a page-level
surface — fixed position, stacked, above the host's modals — while the element is an inline-block
bell inside someone's sticky header, so its toasts would be trapped in that stacking context or
portalled somewhere `::part` cannot reach. It would roughly double the element's public contract
(positioning, duration, stacking, dismissal, pause-on-hover) to reimplement what every design system
already ships. And the capability already exists for a framework-agnostic consumer: `hermes-notification`
bubbles, is composed, and now carries `metadata`. Recorded because it will be re-proposed.

**A native `M` attribute for DynamoDB.** More idiomatic, and queryable. Rejected: DynamoDB carries
numbers as decimal strings in `N`, so a value written through one store and read through the other
would come back as a different Go type. One JSON codec for both keeps the stores honest.
