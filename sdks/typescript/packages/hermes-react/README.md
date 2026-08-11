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

## Headless

For your own markup, with the same state implementation:

```tsx
const inbox = useHermesInbox(useHermes(), { pageSize: 20 });
// notifications, unreadCount, hasMore, loading, error, realtime
// markRead, markUnread, archive, unarchive, remove, markAllRead, loadMore, refresh, clearError
```

`useUnreadCount(client)` gives just the count for a badge elsewhere in your layout. It is correct
from first paint, not only after the user's first action.

## Server rendering

Safe to import from a server component. It renders `<hermes-inbox></hermes-inbox>` with no children
and the element upgrades on hydration, so there is no mismatch and nothing to configure.

The element class is loaded through a browser-only dynamic import, because `LitElement extends
HTMLElement` is evaluated at import time and Node has no `HTMLElement`. `src/ssr.test.tsx` runs in a
node environment specifically to keep that true — a runtime reference to the element class from this
module would crash every server render, and that suite is what catches it.

One consequence worth knowing: the widget is invisible until hydration, since a custom element's
shadow content is never part of React's tree.
