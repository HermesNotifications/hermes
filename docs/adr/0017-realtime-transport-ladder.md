# ADR 0017: Fall back from WebSocket to HTTP-streaming then SSE, derived from one URL

**Status:** Accepted (2026-08-11)  
**Date:** 2026-08-11  
**Author:** Daryl Robbins

---

## Context

Centrifugo predates this ADR log, so no recorded decision covered the realtime transport at all.
What shipped was the implicit one: WebSocket, and only WebSocket. `RealtimeConnection` built a
single endpoint string and handed it to `centrifuge-js`; neither the kustomize overlays nor the
Helm chart set any other transport key.

That is fine until a client sits behind something that breaks the WebSocket upgrade — a
TLS-intercepting corporate proxy, or a middlebox that strips `Upgrade`. The resulting failure is
the worst shape a failure can take, because it looks like success:

- `store.start()` fetches the first page over REST, which works, so the inbox renders normally.
- `waitUntilConnected` times out after 3s and the store carries on, by design.
- The socket never opens, so it never *re*-opens, so `InboxStore.reconcile()` — which is our entire
  gap-repair mechanism — is never triggered. Nothing polls; the unread-count endpoint is
  explicitly documented as not for polling.

The user's inbox is then frozen for the rest of the session with no error in any log, ours or
theirs. It reproduces on one network and nowhere else, and reaches us as "notifications seem
delayed". We could not have detected it: our CI, our monitoring and our own browsers all speak
WebSocket fine.

Centrifugo has offered a fix since v4 that we simply had not turned on.

## Decision

**The browser client tries three transports in order — `websocket`, then `http_stream`, then
`sse` — and keeps the first that connects.** `centrifuge-js` is constructed with the endpoint
array rather than a single string.

**All three endpoints are derived from the existing `socketUrl`.** The public contract stays one
URL: `<hermes-inbox api-url="…" socket-url="…">` is unchanged, and there is no `transports`
attribute. Configuring three URLs would be three chances to get one wrong, and the wrong one would
only fail on networks the integrator cannot reproduce.

**It is on by default, with no opt-in flag.** An opt-in would be found by exactly the integrators
who do not need it. WebSocket still wins whenever it works, so a healthy network sees no change.

**Both fallbacks are enabled server-side in every deployment path we ship** — the kustomize base
and local overlay (v5, flat `"sse": true`) and the Helm chart (v6, nested `sse.enabled`). The key
shape differs by major version and an unrecognised key is *ignored*, not rejected, so both were
verified against the actual pinned images rather than the docs.

**The realtime ingress pins `proxy-buffering: "off"`.** It was already off by default, which means
the ladder worked by luck; a cluster-wide ConfigMap enabling buffering would stall both fallback
rungs with nothing in any log.

**Nothing else changes.** No connect or subscribe proxy, no reads or mutations moved onto the
channel, no new CORS surface on any Hermes service.

## Consequences

**Good.** The silent-freeze failure mode is gone for the networks that cause it. Coverage is broad:
`http_stream` and `sse` are plain HTTP requests, so anything that permits ordinary HTTPS to the
Hermes domain can now receive realtime. No new infrastructure — both ingresses already route
`/realtime(/|$)(.*)`, which covers `/connection/http_stream`, `/connection/sse` and the
`/emulation` endpoint the two share. No session affinity is needed: each client→server command is
an independent POST that any Centrifugo node can answer, so round-robin stays correct. And no CORS
middleware is added to `hermes-inbox` or `hermes-user` — Centrifugo answers its own preflights.

**`allowed_origins` becomes more load-bearing.** It previously governed only Centrifugo's WebSocket
`Origin` check. It now also governs CORS for the two HTTP transports, so a wrong value fails three
transports instead of one. The mitigation is documentation, since the setting was already mandatory
and already documented as the one whose absence is invisible.

**A public type changed.** `TransportFactory` now receives `TransportEndpoint[]` instead of
`string`. This is a breaking change to an exported type under [ADR 0013](0013-embeddable-inbox-widget-contract.md)'s
versioned contract, affecting only consumers who inject a custom factory — a test seam, not
something an integrator uses. Acceptable at 0.1.0, which is unpublished. `transportEndpoints()` is
exported alongside it so a custom factory can reuse the derivation.

**Diagnosis gets one step harder.** "Realtime is connected" no longer implies "over a WebSocket",
so a latency or throughput regression can mean *more clients on a lower rung* rather than a slower
system. Centrifugo's own metrics are labelled by transport, which is where that question is
answered.

**SSE has a connection-cap caveat.** `EventSource` competes for the browser's six connections per
origin under HTTP/1.1. It is a non-issue over HTTP/2, which is what the TLS ingress serves in
staging and production, and it is the third rung regardless — reached only when `http_stream`,
which has no such limit, has also failed.

## Alternatives considered

**Long polling.** Not available. Centrifugo's only long-polling transport was SockJS, deprecated in
v5 and removed in v6. Its replacement is the emulation layer used here, which is more performant,
supports binary, and — decisively for us — needs no sticky sessions, where SockJS's long-polling
did. Adopting SockJS would have meant pinning Centrifugo to v5 and adding session affinity to an
ingress that deliberately has none.

**A polling rung below SSE**, refetching `/v1/inbox` on a timer when no transport connects.
Rejected for now. It is the only rung that survives Centrifugo being down entirely, and
`InboxStore.reconcile()` is most of the machinery already. But it introduces a second source of
truth for "am I live", a timer to tune, and load proportional to broken clients — and it earns its
place only if telemetry shows clients reaching the bottom of the ladder. Revisit with that data.

**WebTransport.** Rejected: experimental in Centrifugo and requires HTTP/3 end to end. It is a
potential rung *above* WebSocket, not a fallback below it, so it addresses a different problem.

**An opt-in `transports` attribute.** Rejected: see the Decision. It puts the burden on the people
least able to know they need it.

**Extending the ladder to the Go CLI.** Rejected, deliberately rather than by omission.
`internal/cli/inbox_ws.go` uses `centrifuge-go`, which has no emulation support. The CLI runs on
operator machines and CI, not inside the corporate browser proxies this ADR exists for, so the
missing rungs cost nothing there. It stays WebSocket-only.

**Enabling the unidirectional variants** (`uni_sse`, `uni_http_stream`). Rejected: they cannot
carry the subscribe command, so the client could not join its own channel. The bidirectional
emulation is the one that replaces a WebSocket.
