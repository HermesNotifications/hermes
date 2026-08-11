---
id: 0010
title: Ship one inbox implementation as a custom element with a versioned public contract, wrapped rather than reimplemented for React
status: Accepted
affects:
  - sdks/typescript/packages/hermes-client/**
  - sdks/typescript/packages/hermes-web/**
  - sdks/typescript/packages/hermes-react/**
  - examples/**
  - tests/browser/**
  - Makefile
source: docs/reviews/2026-07-27-architecture-review.md — finding 46; web inbox hardening pass 2026-07-30
---

# ADR 0010: The embeddable inbox contract

**Status:** Accepted (2026-07-30)  
**Date:** 2026-07-30  
**Author:** Daryl Robbins

---

## Context

`@hermes-notifications/web` already contained a Lit-based `<hermes-inbox>` element, and
`@hermes-notifications/react` contained hooks. Both shipped, neither was documented, and no ADR
covered either. Reading them turned up a set of problems that share one root cause: there was no
recorded decision about what the widget *is*, so each package solved the same problems separately.

- **Inbox state existed twice.** Realtime-event synthesis and optimistic patching were duplicated
  verbatim between the element and the hooks, and had already diverged (one floored the unread count
  at zero, the other did not). That duplication is what shipped a build break when `group_id` was
  renamed to `category_id`: both copies constructed a `Notification` with the old field, and only
  `ci-web.yml` noticed, because `make sdk-ts-build` covered just two of the four packages.
- **It was not actually embeddable.** `build` was bare `tsc`, so `dist/index.js` carried unresolved
  bare specifiers for `lit` and `centrifuge`. There was no artifact a `<script>` tag could load,
  which is the headline use case for a custom element.
- **Events could not leave the element.** They were dispatched without `composed: true`, so they
  never crossed the shadow boundary. No ancestor listener could see them, which makes a React
  wrapper impossible in principle.
- **The package could not be imported on a server.** `@customElement` called
  `customElements.define` at module-import time, so any Next.js server component that transitively
  reached it crashed before rendering.
- **Nothing network-facing was tested.** The element constructed its own client internally with no
  injection seam; its test file said so and stopped at the render boundary.

## Decision

**One implementation of the inbox UI: the `<hermes-inbox>` custom element.** Its attributes,
properties, events, CSS `part`s and CSS custom properties are a public contract, versioned with the
package. Breaking any of them is a breaking release.

**All inbox state lives in one pure reducer in `@hermes-notifications/client`.** `inboxReducer` plus
`InboxStore` are driven by both the element and the React hooks; neither keeps its own copy. The
reducer takes no clock — every action that stamps a timestamp carries `at` — so its transitions are
asserted against exact values rather than "some string was written".

**React wraps the element; it does not reimplement it.** `@hermes-notifications/react` exports a
`HermesInbox` component that renders the custom element and bridges props to properties and
CustomEvents to callbacks, plus `useHermesInbox` as a headless API for teams building their own
markup. There is deliberately no second React-DOM rendering of the widget.

**Registration is an explicit, guarded call.** `registerHermesInbox()`, or importing
`@hermes-notifications/web/define`. The package root is side-effect free and safe to import
anywhere, including on a server.

**We break the repo's zero-bundler convention for `@hermes-notifications/web` alone**, using
`esbuild`'s CLI with no config file, to emit one self-contained minified ESM artifact at
`dist/hermes-inbox.js` (110 kB, 32 kB gzipped). Without it the stated goal is unbuildable.

**Cross-origin embedding is not yet supported.** The services ship no CORS headers, so the only
integration today is a same-origin proxy in the host application's own backend. `examples/demo-server`
is the reference implementation. Adding CORS middleware changes the security surface of two public
services and gets its own ADR.

## Consequences

**Good.** One tested state implementation instead of two divergent ones — `hermes-client` went from
zero tests to 240. The network-facing half of the widget is testable at last, via an injectable
client and a headless controller. A `<script type="module">` tag is now a real integration path. The
React package is SSR-safe, verified by a node-environment suite that reproduces the exact
`ReferenceError: HTMLElement is not defined` crash if the element class re-enters the server's module
graph. Pagination, `action_url` rendering, keyboard access and token refresh exist where they did not.

**Costs and breaking changes.** `import "@hermes-notifications/web"` no longer registers the element.
All three events were renamed to a `hermes-` prefix and now bubble, so a host with a broad delegated
listener will start seeing them. Relative-time output changes for items older than seven days, having
adopted the admin portal's buckets. Rows are `<button>`/`<a>` rather than `<div>`, so a consumer
styling `::part(notification)` may need adjusting. The element's `@state` fields moved into the
controller. These are acceptable at 0.1.0, which is unpublished.

**Accepted limitations.** The React widget is invisible until hydration, because a custom element's
shadow content is never part of React's tree — fine for a notification bell, and documented rather
than hidden. `notification.new` carries no `category_id`, `organization_id` or `user_id`, so a row
synthesized from a live arrival has empty strings there; the honest fix is a richer server payload,
tracked separately. Aliasing the element under a second tag name registers a subclass, because
`customElements.define` refuses one constructor under two names.

> **Update 2026-08-10 ([ADR 0011](0011-cache-first-unread-count.md)).** The count half of that
> richer payload now exists: `notification.new` carries an optional `unread_count`, so the
> reducer no longer has to invent one by incrementing locally — which is the arithmetic that
> diverged between the two implementations this ADR consolidated. It is optional because the
> publishing worker has no database and cannot know the number when its cache has expired. The
> identity fields (`category_id`, `organization_id`, `user_id`) are still absent.

## Alternatives considered

**A fifth `inbox-core` package** for the shared reducer. Rejected: a new CI build target, lockfile
importer and published artifact to isolate ~200 lines both existing consumers already reach through
`hermes-client`.

**Standalone helper functions** rather than a reducer. Rejected: each consumer would still own "and
now update the unread count", and count coordination *is* the logic that had diverged. Helpers move
the easy half and leave the hard half duplicated.

**A second, native React-DOM widget** so a host could restyle it with Tailwind directly. Rejected: two
markup trees, two sets of accessibility wiring, two chances to diverge — a larger duplication than the
one being removed, merely relocated. The need it serves is already met by the headless hooks.

**`@lit/react`'s `createComponent`** for the wrapper. Rejected on evidence: it needs the element class
at module scope, and `LitElement extends HTMLElement` is a class expression evaluated on import, so it
cannot be SSR-safe. The hand-written wrapper is about forty lines, drops a dependency, and lets the
element load as a lazy chunk.

**`vite build --lib`** instead of esbuild. Rejected: it needs a `vite.config.ts`, and vitest reads
that file by default — so a build config would silently change how every test in the package runs.

**`tsup`.** Rejected: its own dependency subtree for no capability esbuild's CLI lacks here.

**Keeping zero bundlers** and telling consumers to bring their own. Rejected: that is the status quo,
and it means the goal is not met.

**A local nginx ingress CORS annotation** to make the demo cross-origin. Rejected: it would make local
more permissive than production, so the demo would work locally and fail the moment anyone pointed it
at staging.
