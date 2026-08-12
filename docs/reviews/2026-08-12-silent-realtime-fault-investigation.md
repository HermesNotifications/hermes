# Investigation — the silently deaf realtime client, 2026-08-12

How a fault that looked like flaky infrastructure was traced to eleven lines of client
lifecycle code, and why the intuitive fix makes it worse.

**Investigated at:** `5398917`, a branch off `9cd20e3` (main), before
[PR #118](https://github.com/darylrobbins/hermes/pull/118) landed  
**Symptom:** ~20–50% of browser tests that wait for the unread badge after a send time out  
**Method:** websocket frame capture in a real browser, then instrumented orderings correlated
against outcomes. Every claim below was measured, not inferred  
**Outcome:** the cause is what [ADR 0018](../adr/0018-client-lifecycle-dispose-is-terminal.md)
records and fixes. This document is the investigation, not the decision

---

## Why this is written down

The fix already exists and is recorded. This is here for a different reason: the fault was
**invisible from every angle a person would naturally check**, and it was found twice, independently,
from opposite directions — once by reading the lifecycle contract (which produced ADR 0018), and
once by chasing a flaky test suite with no knowledge of that work.

The second route left evidence the first did not need, and that evidence is what a future reader
will actually have when this class of fault recurs in some other guise. A recurrence will not
announce itself as "a lifecycle contract problem". It will announce itself as flaky tests.

## The symptom, and why it resisted

A test would do this, and time out at the third line:

```ts
await waitForRealtimeReady(demoPage);
await hermesUser.send({ title: "…", body: "…" });
await expect(badge(demoPage)).toHaveText("1");   // 20s, "element(s) not found"
```

Everything an operator would look at was healthy:

- The socket was **open**, and had genuinely reconnected and resubscribed.
- The status indicator read **`connected`**, truthfully.
- Centrifugo logged the subscription, with no permission errors.
- The notification was **in the database within a second** — the pipeline was never slow.
- No error appeared in the browser console, the client, or any service log.

It also failed *intermittently*, and a differently-shaped subset each run, which is the signature
that sends people looking at infrastructure. A great deal of time went into Centrifugo, NATS, the
ingress and machine load before any of it was ruled out.

## What was ruled out, and how

Recording the dead ends matters as much as the answer, because each one is a plausible place to
start again next time.

| Hypothesis | How it was excluded |
|---|---|
| The pipeline is slow under load | Direct REST probe: send, then poll `GET /v1/inbox`. Arrival in **under 1s, 3 of 3** |
| Publications are lost between Centrifugo replicas | Scaling to a single replica made it **worse**, not better; both replicas logged `Nats Broker connected` |
| Subscriptions are being rejected | Only 2 `permission denied` entries in 10 minutes of runs, both from `multi-user-isolation.spec.ts`, which expects them |
| The demo's login fixture is broken | A hardcoded `localhost:8899` did exist and was fixed — but it caused a *total* failure at setup, not this intermittent one |
| The metadata/toast work introduced it | It hits pre-existing specs (`realtime-arrival`, `multi-user-isolation`) identically. `git log -S` puts the offending line in `3ab92af`, an ancestor of the branch point |

## The decisive evidence

Arming `page.on("websocket")` **before** navigation — the ordering matters, a listener attached
afterwards misses the frames that decide the case — produced this:

```
[206ms] recv {"id":2,"subscribe":{}}                  <- channel subscribed
[206ms] sent HTTP GET /v1/inbox
[215ms] recv HTTP 200 /v1/inbox RESPONSE              <- initial list done
[229ms] recv {"push":{…,"type":"notification.new","unread_count":1}}

store state: {"unreadCount":0,"notifications":[],"realtime":"connected"}
```

**The publication arrives. The client throws it away.** That single fact eliminates every upstream
component at once, and explains why the hunt through Centrifugo, NATS and the ingress found nothing:
all of them were doing their jobs correctly.

Instrumenting the two possible orderings and correlating them against the outcome removes the last
doubt that this is a race rather than a coincidence:

| ordering | result |
|---|---|
| `disconnect` (from dispose) → `store.start` | **pass, 6/6** — `store.dispatchRealtime` fires |
| `store.start` → `disconnect` | **fail, 4/4** — publication parses, one handler present, store never notified |

## Root cause

`HermesClient.dispose()` cleared the handler arrays — `notificationHandlers`, `updateHandlers`,
`unreadCountHandlers` — belonging to consumers that were **still alive**.

`useHermesClient` called `dispose()` from an effect cleanup. React re-invokes that cleanup on a live
instance under StrictMode, which the demo enables deliberately. Any consumer that had already
registered lost its handlers, with nothing to re-register them. The socket then reconnected and
resubscribed perfectly normally — which is precisely why the status stayed truthful and green.

The intermittency comes from `<hermes-inbox>`'s controller starting its store from an **async**
`rebuild()`. Whether `store.start()` registers before or after the cleanup is a race decided by
machine load, which is why it presented as flakiness rather than as a bug.

The general fault, stated in ADR 0018 and worth repeating here: **an effect cleanup must be undoable
by re-running the effect.** Dropping third-party handlers never can be, because the client cannot
know who registered them.

## The intuitive fix is the wrong one

Two remedies suggest themselves. Both were tried.

**Rebuild the client React-side after disposing it.** This is the one most people will reach for,
and it is worse than the disease: **14 of 16 runs failed**, against 4–6 of 16 before. Client churn
multiplies the window rather than closing it. Reverted.

**Stop `dispose()` clearing consumer handlers.** This works, and was the conclusion this
investigation reached — but ADR 0018, written concurrently, considered and rejected it on design
grounds: it leaves no way to actually retire a client, and `dispose` would then name an operation
that disposes of nothing. **The shipped fix is ADR 0018's**: `dispose()` stays terminal,
`useHermesClient`'s unmount cleanup calls `disconnect()` instead, and `InboxStore` repairs its own
wiring. That is a better answer than the one this investigation arrived at, and no change from this
investigation was merged.

## Outcome

Measured on a stack carrying ADR 0018's fix, with retries disabled:

| | before | after |
|---|---|---|
| Full browser suite | 47 passed / 9 flaky / 3 failed | **64 passed / 0 flaky / 0 failed** |

## What this does not close

`waitForRealtimeReady` (in `tests/browser/fixtures/demo.ts`) gates on the status reading
`connected`. That is exactly the signal which read true while the client was deaf.

The cause is fixed, so the gate is adequate today. But the gate still cannot distinguish *connected*
from *will actually deliver*, and a health signal that cannot detect the fault it is watching for is
worth knowing about. Making it prove delivery would change what `hermes-connected` promises, which is
public contract under [ADR 0013](../adr/0013-embeddable-inbox-widget-contract.md) — so it is recorded
here rather than changed in passing.

## Reproducing this class of fault

If realtime is silently dead again, this is the order that pays off:

1. **Check whether the frame arrives**, before anything else. `page.on("websocket")` armed before
   `page.goto`, and log `framereceived`. If the push is in the log, everything upstream is fine and
   the entire server side can be set aside.
2. **Compare the frame against the store's state.** Read `document.querySelector("hermes-inbox").state`.
   Publication present but state unchanged means the client discarded it.
3. **Do not trust a green status.** `connected` describes the socket, not delivery.
4. **Suspect ordering before suspecting the network** when the failure rate is neither 0% nor 100%.
   Instrument both orderings and correlate; a 10/10 correlation is cheap to obtain and ends the
   argument.
