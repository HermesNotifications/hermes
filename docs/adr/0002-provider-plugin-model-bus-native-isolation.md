# ADR 0002: Adopt a provider plugin model with bus-native (NATS subject) isolation

**Status:** Accepted  
**Date:** 2026-06-13  
**Author:** Daryl Robbins

---

## Context

Hermes delivers notifications through a fixed set of channels — `email`, `sms`, `inbox` —
each a separate worker binary that implements a single-method `delivery.Provider`
interface. The seam for pluggability already exists (per-channel workers, per-channel
JetStream work queues, a one-method delivery interface), but the model is closed in three
ways that block where the product needs to go:

1. **Channels are hardcoded.** `email`/`sms`/`inbox` appear as `switch ch` blocks in
   `internal/dispatch` (`FilterChannelsForTemplate`, the contact-info filter, and
   `contentForChannel`) and as fixed template columns. Adding a channel means editing core
   dispatch in several places.
2. **Provider selection is static, not routed.** SMTP-vs-SES is a config switch, not a
   per-notification decision — no failover, no cost-aware routing, no fan-out, no per-tenant
   providers.
3. **Delivery status is one-way.** There is no path for asynchronous carrier receipts
   (SES→SNS→SQS, Twilio webhooks) to flow back into the status rollup.

We also want to support **third-party** providers — authored outside the Hermes binary, run
under mixed trust — without giving untrusted code database access or the ability to
fabricate delivery events. That trust boundary, and *how a provider is hosted and
isolated*, is the architecturally significant, costly-to-reverse choice: it dictates the
cross-service contract every future provider depends on.

The full design is in
[`docs/superpowers/specs/2026-06-13-provider-plugin-model-design.md`](../superpowers/specs/2026-06-13-provider-plugin-model-design.md).
This ADR records the two foundational decisions that the rest of that design rests on. It
builds on [ADR 0001](0001-dynamodb-model-via-extenddb.md): the new hot tables the model
introduces (`delivery_attempts`, `delivery_retries`, …) ride the same store abstraction.

## Decision

**1. We will adopt a provider plugin model built on a channel/provider registry.** A
**channel** is the user-facing medium (email, sms, inbox, …); a **provider** is the
pluggable delivery unit (ses, smtp, twilio, …) serving exactly one channel. Channel-specific
knowledge moves out of hardcoded `switch ch` blocks into a registry of channel descriptors
(content schema + required address) and provider manifests. Adding a channel becomes
*registering metadata*, not editing dispatch. The existing one-method provider interface is
the single seam, with **two hosting modes** — in-process built-in (compiled in, enabled by
config) and out-of-process third-party — that a provider can move between without code
changes.

**2. We will isolate providers bus-natively: the NATS subject is the contract.** A provider
consumes its own `delivery.<channel>.<provider>` work queue and publishes only to its own
`delivery.results.*`, `delivery.receipts.*`, and `provider.health.*` subjects. There is no
in-process plugin loading and no Hermes-defined RPC surface between core and a provider —
the wire contract *is* a small set of NATS subjects plus the message schemas on them. Mixed
trust is enforced by **NATS permissions** (scoped credentials derived from the provider's
manifest), not by provider code: a third-party provider never touches the database, never
holds a public ingress port, and cannot publish `notification.events` or a sibling
provider's subjects.

This is delivered in phases (see Scope). **Phase 1 — the change shipping with this ADR —
introduces the `internal/provider` registry and de-hardcodes the three dispatch switches
into registry lookups, with zero behavior change.** The cross-service NATS subject contract,
per-provider routing, and the third-party trust scoping are realized in later phases against
this same model.

## Consequences

- A new `internal/provider` package owns the registry, channel descriptors, and provider
  manifests. Built-in channels (email/sms/inbox) and providers (smtp/ses/sms/inbox) are
  registered there; dispatch consults it instead of hardcoded switches.
- The three `switch ch` blocks in `internal/dispatch` are gone. Two thin legacy-struct
  accessors (`Recipient.AddressFor`, `RenderedContent.Field`) remain as the boundary between
  registry string-keys and today's fixed columns; they are removed when content/contacts are
  normalized (design phase 2).
- A new cross-service contract — the `delivery.*`/`provider.*` subject family — becomes a
  public, versioned surface (AsyncAPI/proto) that providers depend on. Changing it later is
  a breaking change, which is precisely why it is recorded here.
- New long-running services are planned against this model: a **Redelivery** service (central
  fallback + long-horizon retry), a **Receipt Correlator**, and a shared **Inbound Gateway**.
  Each lands in its own phase.
- A public Go SDK (`hermesplugin`) becomes a maintained artifact; non-Go authors target the
  published wire schema.
- Routing, retry, and fan-out are **platform responsibilities**, never a provider's —
  providers stay "attempt one delivery, return a classified error." This keeps the trusted
  and untrusted cred surface identical and minimal.
- Operational cost: per-provider work queues + DLQ partitions, scoped NATS JWT issuance
  (Hermes as account issuer), and provider health/liveness state to operate.

## Alternatives considered

- **Subprocess gRPC (HashiCorp go-plugin).** A provider as a subprocess speaking gRPC over a
  handshake. Rejected as the primary model: it couples core to each plugin's lifecycle
  (spawn, health, restart), needs a Hermes-defined RPC surface in addition to the message
  schemas we already need, and does not naturally give us the durable work-queue, retry, and
  DLQ semantics that JetStream provides for free. Bus-native reuses the messaging backbone
  Hermes already depends on and makes the trust boundary a credential/permission question.
- **In-binary dynamic loading (Go `plugin` / WASM).** Loading provider code into the Hermes
  process. Rejected as primary: Go `plugin` is brittle (exact toolchain/version coupling, no
  unload, poor cross-platform story) and neither option gives process isolation for untrusted
  code. WASM is retained as a *future* option for untrusted-no-deploy compute, not the day-1
  mechanism.
- **Keep the closed model (status quo).** Rejected: it cannot satisfy de-hardcoded channels,
  routed/failover provider selection, inbound receipts, or third-party providers — the
  reasons this work exists.

## Scope / Rollout

The model is delivered in the phases defined in the design doc. Phase 1 (this PR) is the
registry seam and de-hardcoding only — no NATS contract change, no new service, no behavior
change. Later phases add: normalized content/contact tables; routing policies + per-provider
subjects + the `DeliveryPlan`; the Redelivery service; the inbound receipt pipeline + Inbound
Gateway; the third-party SDK + token-gated self-enrollment + health reporting; per-tenant
overrides; and the circuit breaker.

Several later phases are independently architecturally significant and will get their own
ADRs in their implementing PRs (normalized content/contact model; central routing/retry +
Redelivery service; inbound receipt pipeline & correlation boundary; provider lifecycle —
enrollment, health, breaker), each refining — not superseding — the model decided here.

**Revisit trigger.** Re-evaluate the bus-native choice if the wire contract proves
insufficient for a real provider's needs (e.g. a provider that genuinely requires
synchronous request/response semantics the subject model can't express), or if operating
per-provider work queues + DLQ partitions at scale proves more costly than a subprocess-RPC
alternative would have been.
