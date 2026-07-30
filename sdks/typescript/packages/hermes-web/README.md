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

## React

Use [`@hermes-notifications/react`](../hermes-react), which wraps this element so props arrive as
real properties and events as callbacks.
