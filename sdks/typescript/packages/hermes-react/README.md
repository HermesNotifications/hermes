# @hermes-notifications/react

React bindings for the Hermes notification inbox.

Full documentation: **[docs/embedding-the-inbox.md](../../../../docs/embedding-the-inbox.md)**.

## Install

```bash
pnpm add @hermes-notifications/react @hermes-notifications/web
```

`@hermes-notifications/web` is an optional peer: you need it for the `<HermesInbox>` widget, but not
if you only want the headless hooks.

## The widget

```tsx
import { HermesProvider, HermesInbox } from "@hermes-notifications/react";

<HermesProvider config={{ apiUrl: window.location.origin, socketUrl, token, getToken }}>
  <header>
    <HermesInbox pageSize={20} onNotification={(event) => toast(event.title)} />
  </header>
</HermesProvider>
```

`HermesProvider` is worth using even for one widget: everything below shares one client, and
therefore one websocket.

This wraps the `<hermes-inbox>` custom element rather than reimplementing it — one implementation of
the inbox UI, not two. The wrapper is what makes it usable from React at all: React 18 stringifies
every prop, and even React 19 does not wire `on*` props to CustomEvents.

## Toasts

Provider-agnostic: this package renders no toast UI and depends on no toast library.

```tsx
import { useHermes, useHermesToasts } from "@hermes-notifications/react";
import { sonnerAdapter } from "@hermes-notifications/react/sonner";
import { Toaster } from "sonner";

function Toasts() {
  useHermesToasts(useHermes(), { toast: sonnerAdapter });
  return <Toaster position="top-right" />;
}
```

`sonner` is an **optional peer dependency** and the adapter is a separate subpath, so importing
`@hermes-notifications/react` never pulls it in. To use something else, pass an object with
`info`/`success`/`warning`/`error`/`show` (and optionally `dismiss`) — that is the entire contract.

A notification toasts when it carries `metadata.toast === true`; `metadata.level` picks the method.
Two behaviours worth knowing before you file a bug: **only live websocket arrivals toast** (not the
initial list, and not the REST repair after a reconnect), and **toasts fire whether or not the panel
is open** — use `shouldToast` with `onOpenChange` if you want otherwise. Duplicates are suppressed
by notification id, shared across every hook instance on the same client.

See [Embedding the Inbox](../../../../docs/embedding-the-inbox.md#toasts) for the full contract.

## Headless

For your own markup, with the same state implementation:

```tsx
const inbox = useHermesInbox(useHermes(), { pageSize: 20 });
// notifications, unreadCount, hasMore, loading, error, realtime
// markRead, markUnread, archive, unarchive, remove, markAllRead, loadMore, refresh, clearError
```

`useUnreadCount(client)` gives just the count for a badge elsewhere in your layout. It is correct
from first paint, not only after the user's first action.

## Sharing a client, and retiring one

`useHermesClient` owns the client for the lifetime of your component and needs nothing from you. If
you manage one yourself, the distinction that matters is:

| | Effect | Use it when |
|---|---|---|
| `client.disconnect()` | Closes the socket, **keeps every handler**. Reconnects on the next `connect()`. | Anywhere a lifecycle might run it spuriously — effect cleanups, unmount, going offline. |
| `client.dispose()` | **Terminal.** Closes the socket, drops every handler, and refuses to reconnect. | Only when the client itself is finished, e.g. you are replacing it. |

Reach for `disconnect()` by default. `dispose()` is deliberately unforgiving: a shared client may
have handlers registered by an embedded `<hermes-inbox>` or another component, and disposing takes
those with it — so a subsequent `connect()` rejects rather than handing you a socket that receives
publications and delivers them to nobody. That failure used to be possible and silent; see
[ADR 0018](../../../../docs/adr/0018-client-lifecycle-dispose-is-terminal.md).

## Server rendering

Safe to import from a server component. It renders `<hermes-inbox></hermes-inbox>` with no children
and the element upgrades on hydration, so there is no mismatch and nothing to configure.

The element class is loaded through a browser-only dynamic import, because `LitElement extends
HTMLElement` is evaluated at import time and Node has no `HTMLElement`. `src/ssr.test.tsx` runs in a
node environment specifically to keep that true — a runtime reference to the element class from this
module would crash every server render, and that suite is what catches it.

One consequence worth knowing: the widget is invisible until hydration, since a custom element's
shadow content is never part of React's tree.
