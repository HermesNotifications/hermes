# Embedding the Inbox

Hermes ships a notification inbox you can drop into any web application: a bell with an unread
badge, a panel listing notifications, live updates over a websocket, and mark-read/archive actions.

It is a **custom element**, so it works in any framework or none. React gets a native binding over
that same element.

> If you want to build your own inbox UI against the raw HTTP API, see the
> [Integration Guide](integration-guide.md) instead. This document is for using the supplied widget.

---

## The three things it needs

| Input | What it is |
|---|---|
| An **API origin** | Where the inbox API is reachable *from the browser*. Usually your own origin — see [Origins and CORS](#origins-proxying-and-cors). |
| A **user token** | A short-lived Hermes JWT, minted by your backend. |
| A **socket URL** | Where the realtime service is reachable, for live updates. One URL — see [How live updates connect](#how-live-updates-connect). |

There is no fourth thing. In particular there is **no user id to configure** — the element reads the
internal Hermes user id from the token's `sub` claim.

> **Why that matters.** Centrifugo channels are named `user#<sub>`, where `sub` is the *internal*
> Hermes id, not the external id you passed when minting the token. Subscribing with the wrong one
> fails in the most awkward way possible: REST keeps working, so the inbox loads and looks perfectly
> healthy, while the subscription is rejected and no update ever arrives. Letting the SDK derive it
> removes the entire class of error. You can still pass `user-id` explicitly if you need to.

---

## How live updates connect

The widget tries three transports in order and keeps the first that connects:

| | Transport | When it is used |
|---|---|---|
| 1 | WebSocket | Almost always. Nothing below this runs on a healthy network. |
| 2 | HTTP-streaming | The `Upgrade` handshake was blocked or rewritten — typically a TLS-intercepting corporate proxy. |
| 3 | SSE | HTTP-streaming was also blocked. |

All three are derived from the single `socket-url`, so there is nothing extra to configure and no
second URL to keep in sync. All three deliver identical events; nothing about the widget's behaviour
or your integration changes with the rung it lands on.

**Why this exists.** Before the ladder, a user behind a websocket-hostile proxy hit a failure that
looked like success: the inbox loaded its first page over REST and then never changed again for the
rest of the session. No error, no console warning, no reconnect — because a socket that never opens
never reconnects, and nothing else triggers a refresh. The support report is "notifications are
delayed", from one user on one network, and it reproduces nowhere else.

**What it does not do.** There is no polling rung. If all three fail the widget is genuinely offline
and says so via `hermes-status`; it does not fall back to a timer. There is no long-polling either —
the transports above replace it, and they need no session affinity, so they work behind an ordinary
round-robin load balancer.

If you self-host, both fallbacks must be enabled server-side; see
[Self-hosting → Configuration](self-hosting/configuration.md). The bundled chart and the kustomize
overlays both enable them by default.

---

## Quickstart: any framework, no build step

```html
<script
  type="module"
  src="https://cdn.jsdelivr.net/npm/@hermes-notifications/web@0.1.0/dist/hermes-inbox.js"
  integrity="sha384-…"
  crossorigin="anonymous"
></script>

<hermes-inbox
  api-url="https://app.example.com"
  socket-url="https://hermes.example.com/realtime"
  token-url="/api/hermes/session"
></hermes-inbox>
```

That is the whole integration. The bundled artifact is self-contained (~110 kB, ~32 kB gzipped) and
includes Lit and the Centrifugo client, so nothing else needs installing.

Pin the version and use Subresource Integrity, as above — a floating `@latest` from a CDN is a script
you have not reviewed executing on your origin with access to your users' sessions. Take the hash
from the published release, or self-host the file from your own assets and skip the CDN entirely.

`token-url` rather than `token` is deliberate: tokens expire, and a URL is the only refresh mechanism
plain HTML can express. See [Tokens](#tokens-and-refresh).

### With a bundler

```bash
pnpm add @hermes-notifications/web
```

```ts
import { registerHermesInbox } from "@hermes-notifications/web";

registerHermesInbox();
```

Registration is an explicit call, not an import side effect, so the package is safe to import during
server rendering. `import "@hermes-notifications/web/define"` does it for you if you prefer.

---

## React

```bash
pnpm add @hermes-notifications/react @hermes-notifications/web
```

```tsx
import { HermesProvider, HermesInbox } from "@hermes-notifications/react";

function AppShell({ session }) {
  return (
    <HermesProvider
      config={{
        apiUrl: window.location.origin,
        socketUrl: session.socketUrl,
        token: session.token,
        getToken: async () => (await fetchSession()).token,
      }}
    >
      <header>
        <HermesInbox
          pageSize={20}
          onNotification={(event) => toast(event.title)}
          onAction={(notification, event) => {
            event.preventDefault();       // stop the browser navigating…
            router.push(notification.action_url!); // …and route internally instead
          }}
        />
      </header>
    </HermesProvider>
  );
}
```

`HermesProvider` is worth using even for a single widget: everything below it shares one client and
therefore one websocket. Two components each building their own client would open two connections
for the same user. A client you pass in stays yours: the widget never disposes it, so a badge or a
second widget reading the same client keeps working when this one unmounts or rebuilds.

> **`action_url` is restricted to `http:`, `https:` and relative URLs.** It reaches the widget as an
> unvalidated string from the send API and over the websocket, and the row renders it into an
> `href` — so anything else (`javascript:`, `data:`, `vbscript:`) would be script execution in
> *your* page. A notification whose `action_url` fails that check renders as a plain row with no
> link and no action label; it still emits `hermes-notification-click`.

The wrapper exists because React does not handle custom elements well — React 18 stringifies every
prop, and even React 19 does not connect `on*` props to CustomEvents. Through the wrapper,
`pageSize={20}` arrives as the number `20` and `onNotification` is actually called.

**Server rendering.** The React package is safe to import from a server component. It renders
`<hermes-inbox …></hermes-inbox>` with no children, and the element upgrades on hydration — so there
is no hydration mismatch, and no configuration needed. The one consequence worth knowing: the widget
is invisible until hydration, since a custom element's shadow content is never part of React's tree.

### Building your own UI

If you want your own markup, skip the widget and use the headless hook. It is the same state
implementation, so nothing is lost.

```tsx
const inbox = useHermesInbox(useHermes(), { pageSize: 20 });

// inbox.notifications, .unreadCount, .hasMore, .loading, .error, .realtime
// inbox.markRead(id), .archive(id), .markAllRead(), .loadMore(), .refresh()
```

`useUnreadCount(client)` gives just the count, for a badge elsewhere in your layout. It is correct
from first paint, not only after the user's first action.

---

## Tokens and refresh

Your backend mints tokens; the browser never sees your API key.

```
Browser                Your backend                    Hermes Admin API
   |  GET /api/hermes/session  |                              |
   |-------------------------->|                              |
   |                           | POST /v1/auth/token          |
   |                           | Authorization: <API key>     |
   |                           |----------------------------->|
   |                           |<-- { token, expires_at } ----|
   |<-- { token, expires_at } -|                              |
```

Tokens live several hours with jitter, so any session that outlives one needs refresh. Three ways,
in precedence order:

1. **`client` property / `HermesProvider`** — you own the client and its `getToken`. Best for React.
2. **`getToken` property** — a callback returning a fresh token. Any JavaScript consumer.
3. **`token-url` attribute** — the element `fetch`es it with credentials, reads `{ token,
   expires_at }`, and renews a minute before expiry. **The only option available to a pure-HTML
   consumer**, and why it exists.

A bare `token` attribute with no refresh mechanism works, and stops working when that token expires.
Fine for a demo; state it explicitly if you ship it.

Your token endpoint should look like this — note the identity comes from your session, never from the
request:

```ts
// Reference implementation: examples/demo-server/src/session.ts
const { token, expiresAt } = await hermes.auth.exchangeToken({
  organizationId: session.organizationId,   // from YOUR session
  userId: session.userId,                   // never from the request body
});
return Response.json({ token, expires_at: expiresAt });
```

---

## Origins, proxying and CORS

**The Hermes services do not send CORS headers.** A browser on your origin therefore cannot call the
inbox API directly, and the supported integration today is to **proxy `/v1/*` through your own
backend**:

```
browser → https://app.example.com/v1/inbox → (your backend) → https://hermes.example.com/v1/inbox
```

Set `api-url` to your own origin — or omit it, since that is the default.

Two rules for the proxy. Both matter:

1. **Forward the caller's `Authorization` header verbatim.** Never substitute one. The proxy is an
   unauthenticated relay for user-scoped endpoints; if the browser sends no token, Hermes must return
   401. Quietly supplying a token would let any visitor read any inbox.
2. **Never attach your Hermes API key to a proxied request.** It belongs only to your token endpoint.
   An API key on a proxied request hands the browser administrative reach over your organization.

A complete implementation is `examples/demo-server/src/proxy.ts`, in about sixty lines with no
dependencies.

**The realtime connection does not go through the proxy.** `socket-url` points straight at
Centrifugo, cross-origin, exactly as in production. If you self-host, set `allowed_origins` to your
app's origin — without it every browser is refused while curl and health checks pass.

That one setting now covers two distinct mechanisms, which is worth knowing when you debug it. The
WebSocket handshake is not subject to CORS at all; Centrifugo enforces `allowed_origins` as its own
`Origin` check. The `http_stream` and `sse` fallbacks *are* ordinary CORS-governed requests, and
Centrifugo answers their preflights from the same list. So a correct `allowed_origins` makes the
whole ladder work cross-origin and no Hermes service needs CORS middleware — but a *missing* one now
fails on three transports rather than one, and the browser reports it as a CORS error on the
fallbacks and an opaque handshake failure on the websocket.

> Cross-origin support without a proxy is a genuine gap for an embeddable widget, and is tracked
> separately — it changes the security surface of two public services and needs its own review. See
> [ADR 0013](adr/0013-embeddable-inbox-widget-contract.md).

---

## Attributes and properties

| Attribute | Default | Purpose |
|---|---|---|
| `api-url` | the page's origin | Where the inbox API is reachable from the browser |
| `socket-url` | `api-url` | Base URL for the realtime connection. One URL: the widget derives its whole transport ladder from it |
| `token` | — | A Hermes user JWT |
| `token-url` | — | Endpoint returning `{ token, expires_at }`; enables auto-refresh |
| `user-id` | the token's `sub` | Override the realtime channel's user |
| `page-size` | `20` | Rows per page (API caps at 100) |
| `archived` | `false` | Show the archived view |
| `open` | `false` | Reflected; whether the panel is open |
| `heading` | `Notifications` | Panel heading |
| `empty-text` | `No notifications` | Shown when there is nothing to list |
| `expand-text` | `Show more` | Label on the control that lifts a clipped row |
| `collapse-text` | `Show less` | Label once that row is expanded |

Properties without an attribute equivalent, since a function cannot be expressed in HTML:
`getToken`, `client`, `clientFactory`, and read-only `state`.

## Events

All bubble and are composed, so you can listen on any ancestor or on `document`.

| Event | `detail` |
|---|---|
| `hermes-notification` | the arriving notification event |
| `hermes-update` | an inbox state change from the server |
| `hermes-unread-count-change` | the new count, as a number |
| `hermes-open-change` | `{ open }` |
| `hermes-connected` | `{ status }` — realtime is live and will deliver |
| `hermes-realtime-change` | `{ status }` — **every** transition, for an honest connection indicator |
| `hermes-error` | a `HermesError` |
| `hermes-action` | `{ notification }` — **cancellable**; `preventDefault()` to route yourself |
| `hermes-notification-click` | `{ notification }` |

`hermes-connected` is the signal to wait on if you need to know the inbox is live. It fires when the
channel subscription is established, which is later than the socket opening and is the moment
delivery is actually guaranteed.

---

## Theming

Two mechanisms, both crossing the shadow boundary.

**CSS custom properties** for colours and metrics:

```css
hermes-inbox {
  --hermes-font-family: inherit;
  --hermes-text-color: #14171f;
  --hermes-badge-bg: #ef4444;
  --hermes-accent-color: #4f46e5;
  --hermes-popover-bg: #fff;
  --hermes-popover-width: 380px;
  --hermes-border-color: #e3e6ec;
  --hermes-popover-z-index: 50;   /* raise above your own sticky header */
  --hermes-focus-ring: 2px solid #4f46e5;

  --hermes-trigger-border: 1px solid #e3e6ec;  /* the bell ships borderless; this puts a box back */
  --hermes-body-line-clamp: 3;                 /* lines of body text before clipping (default 2) */
  --hermes-expand-color: #4f46e5;              /* the "Show more" control */
  --hermes-level-error-color: #dc2626;         /* and -info-, -success-, -warning- */
}
```

`--hermes-popover-z-index` is worth knowing about: if your header has a stacking context above the
default of 1000, the panel is clipped, and this is the lever.

**`::part()`** to restyle internal elements, which custom properties cannot reach:

```css
hermes-inbox::part(trigger) { border-radius: 10px; }
hermes-inbox::part(notification unread) { border-left: 2px solid #4f46e5; }
hermes-inbox::part(notification error) { background: #fef2f2; }
```

Exposed parts: `trigger`, `badge`, `status`, `popover`, `header`, `mark-all-read`, `list`,
`notification` (plus a `read`/`unread` token, an `expanded` token, and a `level-*` token when the
notification declares one), `unread-dot`, `read-dot`, `row-target`, `notification-content`,
`title`, `body`, `time`, `action-label`, `action-link`, `expand-toggle`, `actions`, `action-btn`,
`footer`, `load-more`, `loading`, `empty`, `error`.

> **One `::part()` limitation, so you don't spend an afternoon on it.** `::part()` may appear only
> once, at the end of a compound selector, and nothing may descend past it.
> `hermes-inbox::part(notification):hover` is fine, but there is no way to express "reveal
> `::part(action-btn)` when `::part(notification)` is hovered" — the popular reveal-on-row-hover
> pattern is not available to a consumer, and would have to become the element's own behaviour.

A worked example that goes well beyond a border radius — a circular tinted bell, a gradient panel
header, an accent rail in place of the unread dot — is the `brand` theme in
[`examples/react-demo/src/styles.css`](../examples/react-demo/src/styles.css). Switch the demo's
Theme select to "Brand" to see it.

### Long notifications

A body is clipped to `--hermes-body-line-clamp` lines (2 by default) and the title to one. When a
row is actually clipped — measured, not guessed — a **Show more** control appears beneath it and
lifts both. Rows that fit get no control, so short notifications add no extra tab stop.

Expanding never marks the notification read and never follows its action. That is structural
rather than defensive: the control is a sibling of the row's link/button, not a child of it.

---

## Metadata: levels and toasts

A send may attach an arbitrary `metadata` object. Hermes stores it, echoes it back on the inbox row
and on the realtime event, and reads exactly two keys from it:

```jsonc
POST /v1/send
{
  "to": { "organization_id": "…", "user_id": "…" },
  "content": { "title": "Payment failed", "body": "Your card was declined." },
  "channels": ["inbox"],
  "metadata": {
    "level": "error",     // info | success | warning | error — how it should look
    "toast": true,        // whether it should interrupt rather than wait to be opened
    "invoiceId": "1041",  // yours. Round-trips untouched.
    "tab": "billing"      // also yours.
  }
}
```

**Hermes will never interpret a third key.** That is a commitment, not an implementation detail:
reserving another bare top-level name later would break anyone already using it for their own data.

`level` and `toast` are independent, deliberately. An `error` with no `toast` is styled as an error
in the list without interrupting anyone; an `info` with `toast: true` is a passing confirmation.

A few properties worth knowing:

- **`level` is validated at the send edge.** An unrecognised value is a `422`, naming the allowed
  set — it is not coerced to `info` and not silently dropped, because `level` is optional and so
  only a caller who typed something wrong is affected.
- **Clients treat an unknown `level` as no level.** Levels may be added; a client that predates one
  must stay renderable rather than throw.
- **`toast` is a hint, not a channel.** It does not affect routing, and a client with no transient
  surface may ignore it.
- **The object is capped at 4 KiB** serialized.
- **"Verbatim" means a semantic round trip, not byte-for-byte.** It is stored as `jsonb`, which does
  not preserve key order and collapses duplicate keys; numbers come back as JSON numbers.

The widget renders `level` as a coloured rail on the row, so a severity is still visible when the
user opens the panel later — not only for the few seconds a toast is on screen.

## Toasts

Hermes renders no toast UI and depends on no toast library. React gets a hook plus a small adapter
interface, so you keep whichever toast component you already use.

```bash
pnpm add sonner   # or bring your own
```

```tsx
import { useHermes, useHermesToasts } from "@hermes-notifications/react";
import { sonnerAdapter } from "@hermes-notifications/react/sonner";
import { Toaster } from "sonner";

function Toasts() {
  useHermesToasts(useHermes(), { toast: sonnerAdapter });
  return <Toaster position="top-right" />;
}
```

`sonner` is an **optional peer dependency**, and the adapter lives on its own subpath — importing
`@hermes-notifications/react` never pulls it in, so a host that renders its own toasts pays nothing
and a server component still imports cleanly.

**To swap it**, pass a different object. That is the whole integration:

```ts
const myToaster: HermesToastAdapter = {
  info:    (t) => mySnackbar.show(t.title, { kind: "info" }),
  success: (t) => mySnackbar.show(t.title, { kind: "ok" }),
  warning: (t) => mySnackbar.show(t.title, { kind: "warn" }),
  error:   (t) => mySnackbar.show(t.title, { kind: "error" }),
  show:    (t) => mySnackbar.show(t.title),          // no level, or one we don't know
  dismiss: (handle) => mySnackbar.close(handle),     // optional
};
```

Each method receives the whole payload — `{ id, title, body, level, toastRequested, notification,
event }` — so an adapter that wants to render the action link or the timestamp can.

### Two behaviours people assume are bugs

**Only live arrivals toast.** The hook listens to websocket publications, not to the initial list and
not to the REST repair that runs after a reconnect. Loading a page with forty unread notifications
produces no toasts, and neither does a laptop waking up after an hour.

**Toasts fire whether or not the panel is open.** The SDK cannot know whether it is — a headless host
has no panel. To suppress them while the user is already reading their inbox, track `onOpenChange`
on `<HermesInbox>` and consult it from `shouldToast`:

```tsx
useHermesToasts(client, {
  toast: sonnerAdapter,
  shouldToast: (payload) => payload.toastRequested && !panelOpen,
});
```

Options: `enabled`, `shouldToast`, `dismissOnRead` (retract when read elsewhere — needs `dismiss`),
`dedupeScope`, `dedupeSize`. Duplicate arrivals are suppressed by notification id, shared across
every hook instance driving the same client, so two mounted consumers toast once between them.

### Without React

The custom element emits everything needed; there is no toast machinery to miss:

```js
document.querySelector("hermes-inbox").addEventListener("hermes-notification", (event) => {
  const { title, body, metadata } = event.detail;
  if (metadata?.toast) myToast(metadata.level ?? "info", title, body);
});
```

That is deliberate rather than an omission. A toast is a page-level surface — fixed position,
stacked, above your modals, in your design language — and the element is an inline-block bell
anchored inside your header. Rendering toasts from inside its shadow root would either trap them in
your header's stacking context or portal them somewhere you cannot style.

## Run the demo

A complete working integration lives in [`examples/`](../examples/README.md): a React host
application with the widget in its header, plus the shared token-minting and proxying backend.

```bash
make dev-up        # the full stack
make demo-install  # dependencies and SDK builds
make dev-demo      # the demo on http://localhost:5173
```

Send yourself a notification from the panel in the app, or independently from a terminal, and watch
it arrive without a reload:

```bash
scripts/hermes-local notifications send \
  --organization-id <uuid> --user-id demo-user-1 \
  --title "Invoice ready" --body "Invoice #1041" --channels inbox
```
