# @hermes-notifications/web

The Hermes notification inbox as a custom element. Works in any framework, or none.

Full documentation: **[docs/embedding-the-inbox.md](../../../../docs/embedding-the-inbox.md)**.

## Install

```bash
pnpm add @hermes-notifications/web
```

```ts
import { registerHermesInbox } from "@hermes-notifications/web";

registerHermesInbox();
```

Registration is an explicit call rather than an import side effect, so the package is safe to import
during server rendering — `customElements` does not exist in Node. `import
"@hermes-notifications/web/define"` registers for you.

## No build step

`dist/hermes-inbox.js` is self-contained: Lit, the Centrifugo client and the Hermes client are all
inlined, and importing it registers the element.

```html
<script type="module" src="/path/to/hermes-inbox.js"></script>
<hermes-inbox api-url="https://app.example.com" token-url="/api/hermes/session"></hermes-inbox>
```

**110 kB minified, 32 kB gzipped.** If that number moves materially, check nothing pulled in
centrifuge's protobuf entry point — the JSON codec is the one this uses.

## Entry points

| Import | Registers the element | Use for |
|---|---|---|
| `@hermes-notifications/web` | no | bundlers; safe on a server |
| `@hermes-notifications/web/define` | yes | bundlers, when you want registration |
| `@hermes-notifications/web/standalone` | yes | the pre-bundled file, for a `<script>` tag |

## Attributes, events and theming

See [docs/embedding-the-inbox.md](../../../../docs/embedding-the-inbox.md). In brief: configure with
`api-url`, `socket-url` and one of `token` / `token-url` / a `getToken` property; listen for
`hermes-*` events, which bubble and are composed; theme with `--hermes-*` custom properties and
`::part()`.

There is no user id to configure — the element reads the internal Hermes id from the token's `sub`
claim, which is what the Centrifugo channel is named after.

## Toasts are not this element's job

Deliberately, and it is worth saying out loud so it is not mistaken for an omission. A toast is a
page-level surface — fixed position, stacked, above your modals, in your design language — while
this element is an inline-block bell anchored inside your header. Toasts rendered from inside its
shadow root would be trapped in your header's stacking context, or portalled somewhere `::part()`
cannot reach. It would also roughly double the element's public contract to reimplement what every
design system already ships.

Everything needed is already emitted:

```js
document.querySelector("hermes-inbox").addEventListener("hermes-notification", (event) => {
  const { title, body, metadata } = event.detail;
  if (metadata?.toast) myToast(metadata.level ?? "info", title, body);
});
```

React users get a hook and an adapter interface — see [`@hermes-notifications/react`](../hermes-react).

## React

Use [`@hermes-notifications/react`](../hermes-react), which wraps this element so props arrive as
real properties and events as callbacks.
