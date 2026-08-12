# ADR 0018: `dispose()` is terminal, `disconnect()` is the reusable one, and the store repairs its own wiring

**Status:** Accepted (amended 2026-08-12: independently reproduced from the opposite direction; see
the update at the end of Consequences)  
**Date:** 2026-08-12  
**Author:** Daryl Robbins

---

## Context

`HermesClient` had two teardown operations and no clear line between them.

`disconnect()` closed the socket and kept handlers. `dispose()` dropped every handler consumers had
registered but **deliberately preserved the client's own internal wiring**, so that "the client
still works if it is used again". That second sentence is the problem: a client with no handlers
that still works is a client which connects, subscribes, receives publications, and delivers them
to nobody. Healthy from every angle — socket open, status `connected`, no errors anywhere — and
completely silent.

That combination shipped, and `useHermesClient` walked straight into it:

```ts
useEffect(() => {
  const { client } = current;
  return () => { client.dispose(); };
}, [current]);
```

React StrictMode runs effect → cleanup → effect on the same memoized instance, so this cleanup
fires while the client is still in use. An embedded `<hermes-inbox client={…}>` registers its own
handlers during an async `start()`, roughly 4ms later. Whichever won the race decided whether
realtime worked for the whole session. It presented as flaky realtime — four or five browser tests
failing per run, a different subset each time — and survived a long hunt through Centrifugo,
NATS, transports and ingress before the frame log showed publications arriving at a browser that
did nothing with them.

The general fault is that **an effect cleanup must be undoable by re-running the effect**, and
dropping third-party handlers never can be: the client cannot know who registered them, so nothing
can restore them. Any cleanup that does it is unsafe by construction, not merely unlucky.

## Decision

**`dispose()` is terminal.** It closes the socket, drops every handler, and marks the connection
disposed. `connect()` afterwards rejects rather than producing the zombie above. It is idempotent,
because a lifecycle teardown may legitimately run twice. Call it only when the client itself is
finished — replaced because `apiUrl`/`socketUrl` changed, or torn down by whoever built it.

**`disconnect()` is the reuse-safe operation**, and the one a component lifecycle wants. It closes
the socket and keeps every handler, so running it spuriously costs a reconnect rather than the
client's usefulness.

**A disposed connection cannot be resurrected by an in-flight connect.** `dispose()` is synchronous
and `openConnection()` awaits a token, so dispose can land after a caller asked to connect but
before the transport exists. The disposed flag is re-checked after that await; without it the
socket opens into a client whose handler lists have already been emptied, which is the original bug
reassembled from different parts.

**`useHermesClient`'s unmount cleanup disconnects.** Replacing the client on an identity change
still disposes, because there the client really is finished.

**`InboxStore` repairs its own wiring.** A started, undisposed store that observes a `disconnected`
it did not initiate re-registers its handlers and reconnects. `disconnected` is only ever emitted by
an explicit `disconnect()`/`dispose()` — a transport drop reports `connecting`, because the socket
is coming back by itself — so this fires exactly when another party has torn down a connection the
store still depends on. The repair is deferred to a microtask: it mutates the handler collections
the client is part-way through iterating, and whether that is safe otherwise depends on the
emitter's internals two layers away.

## Consequences

**Good.** The silent-deaf client is gone as a class, not just at this call site. The browser suite
went from four or five failures per run to green. Misuse now fails loudly at the point it is made:
a disposed client rejects instead of pretending. `useHermesClient` gained its first tests — its
having none is exactly how this shipped.

**A breaking change to a public method.** `dispose()` previously allowed reuse; it no longer does.
Under [ADR 0013](0013-embeddable-inbox-widget-contract.md) that is a versioned contract, so this is
a breaking release. Acceptable at 0.1.0, which is unpublished. Two tests that pinned the old
behaviour — `still delivers events after a dispose-then-reuse cycle` and its unread-count twin —
were rewritten to assert the new contract rather than deleted, because the behaviour they described
is precisely what changed.

**The store's repair can override an application.** An app that deliberately disconnects a shared
client while an inbox is mounted will find it reconnected. That precedence is intended: a mounted
inbox asking for realtime is a live claim, and the alternative is the silent deafness this ADR
exists to remove. `stop()` clears `started` *before* it disconnects, so a deliberate teardown is
never fought.

**Defence in depth, not one fix.** The store repair and the hook change each fix the observed bug
alone. Keeping both is deliberate: the hook fix addresses this caller, the repair addresses the
next one.

> **Update 2026-08-12: independently reproduced, from the opposite direction.**
>
> While this ADR was being written, a separate investigation was hunting the same symptom from the
> other end — starting from a flaky browser suite rather than from a reading of the lifecycle
> contract, and without knowledge of this work. It arrived at the same cause. That is worth
> recording, because the two routes leave different evidence and the second kind is what a future
> reader will have when this recurs in some other guise.
>
> **The publication arrives; the client discards it.** With `page.on("websocket")` armed before
> navigation, the frames say so directly — the channel subscribes, the initial list completes, the
> `notification.new` push lands 14ms later, and the store's state is still
> `{"unreadCount":0,"notifications":[]}`. Nothing upstream is at fault, which is why a long hunt
> through Centrifugo, NATS and the ingress found nothing: every one of them was doing its job.
>
> **The race resolves the outcome, 10 times out of 10.** Instrumenting the two orderings and
> correlating them against pass/fail removes the last doubt that this is a race rather than a
> coincidence:
>
> | ordering | result |
> |---|---|
> | `disconnect` (from dispose) → `store.start` | pass, 6/6 |
> | `store.start` → `disconnect` | fail, 4/4 — publication parses, one handler present, store never notified |
>
> **A sixth alternative, measured.** Beyond the five below, one more was tried and abandoned:
> *rebuild the client React-side after disposing it*. It made things markedly worse — 14 of 16 runs
> failed, against 4–6 of 16 before — because client churn multiplies the window rather than closing
> it. It is the intuitive fix, so it is worth knowing it is the wrong one.
>
> **What the fix is worth, end to end.** The full browser suite went from 47 passed / 9 flaky / 3
> failed to **64 of 64 with retries disabled**, measured on a stack carrying this change.
>
> **One thing this does not close.** `waitForRealtimeReady` in `tests/browser/fixtures/demo.ts`
> still gates on the status reading `connected`, and under this bug that read `connected` while the
> client was deaf — the socket genuinely reconnected and resubscribed, so the signal was true and
> meaningless. The cause is fixed, so the gate is adequate today; the gate itself still cannot
> distinguish "connected" from "will actually deliver". Making it prove delivery would change what
> `hermes-connected` promises, which is public contract under
> [ADR 0013](0013-embeddable-inbox-widget-contract.md) — so it is named here as a known limit
> rather than changed in passing.

## Alternatives considered

**Only change the hook to `disconnect()`.** Rejected as insufficient, and this was measured rather
than assumed. It leaves `dispose()`'s contradictory contract in place for the next caller, and on
its own it closes a socket that nothing reopens for the custom-element path — whose controller
starts asynchronously, outside React's effect ordering. It would have converted a silent failure
into a visible one.

**Only add the store repair.** Rejected: it fixes the widget's own state but not the host-facing
callbacks. `InboxController.wireClientEvents` registers through the same `client.on(…)` arrays that
`dispose()` empties, so one `dispose()` wipes two independent sets of registrations and the repair
restores one. Chasing that with a second repair in the controller is the tell that the contract, not
the consumers, is what is wrong.

**Make `dispose()` stop clearing consumer handlers.** Rejected: it would leave no way to actually
retire a client, and "dispose" would name an operation that disposes of nothing.

**Detect a genuine unmount and dispose only then.** Rejected as impossible by construction. React
deliberately makes StrictMode's simulated unmount indistinguishable from a real one; that is the
entire point of the mode.

**Reference-count the shared client.** Rejected as more machinery than the problem needs, and it
would still not survive a consumer that disposes rather than releasing its reference.
