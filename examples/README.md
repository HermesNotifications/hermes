# Integration demos

Working examples of embedding Hermes in a real application. Each one is meant to be read and copied,
not just run.

| Demo | What it shows |
|---|---|
| [`react-demo/`](react-demo/) | A React host application with the inbox widget in its header |
| [`demo-server/`](demo-server/) | The token-minting and proxying backend, shared by every demo |

For the widget's own documentation — attributes, events, theming, token refresh — see
[docs/embedding-the-inbox.md](../docs/embedding-the-inbox.md).

## Running them

```bash
make dev-up        # the full stack: cluster, services, Centrifugo
make demo-install  # dependencies, and build the workspace SDKs
make dev-demo      # token server on :8899, demo app on :5173
```

Then open <http://localhost:5173>. The panel on the right sends test notifications; watch them arrive
in the header without a reload.

To convince yourself the button is not faking it, send from a terminal instead:

```bash
scripts/hermes-local notifications send \
  --organization-id 3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11 \
  --user-id demo-user-1 \
  --title "Invoice ready" --body "Invoice #1041" --channels inbox

# ...and watch the same delivery from a second terminal
scripts/hermes-local inbox listen \
  --organization-id 3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11 --user-id demo-user-1
```

## Why there are two packages

The token mint is a **separate process** from the front-end, because that is what it is in a real
integration: your existing backend holds the Hermes API key, and your front-end never sees it.
Collapsing them into one app would be shorter and would teach the wrong shape.

It also means a demo for another framework is one component plus an `index.html` — the backend is
already framework-agnostic and needs no changes.

`demo-server` does three things:

- **`POST /api/session`** mints a user token. Identity comes from the server-side session **only**,
  never from the request body. It also returns the decoded `sub`, because that is the id the realtime
  channel needs and the browser cannot derive it otherwise.
- **`/v1/*`** proxies to Hermes, forwarding the caller's token verbatim and never attaching the API
  key. This is the CORS workaround, and the supported integration path today — see
  [Origins, proxying and CORS](../docs/embedding-the-inbox.md#origins-proxying-and-cors).
- **`POST /api/demo/login`** sets a signed cookie. This is demo scaffolding standing in for whatever
  session your app already has; replace it entirely.

## What to look at once it is running

- **The send panel** drives `metadata.level` and `metadata.toast` — the two keys Hermes reads.
  The presets cover each level, a long body and a long title (to exercise the **Show more**
  control), and a warning that is deliberately *not* toasted, because presentation and
  interruption are separate decisions.
- **The Theme select** switches between three host themes. `Brand` is the worked restyling
  example: a circular tinted bell, a gradient panel header, and an accent rail instead of the
  unread dot — all from the host's stylesheet, none of it a fork of the widget. The CSS is
  commented in [`react-demo/src/styles.css`](react-demo/src/styles.css).
- **[`react-demo/src/host/Toasts.tsx`](react-demo/src/host/Toasts.tsx)** is the whole toast
  integration: a hook, an adapter, and sonner's `<Toaster/>`. Swapping toast libraries means
  passing a different adapter object.

## Two honest caveats

**The send button is labelled "transactional" for a reason.** It supplies content directly rather than
naming a template, and a direct-content send bypasses preference resolution entirely — the channels
requested are the channels used. A preference toggle next to it would have no effect on it, so the
demo does not pretend otherwise.

**`POST /v1/send` returns 202 before the notification exists.** Dispatch creates the row afterwards,
so there is always a moment between clicking and arriving. The demo shows it as "accepted, awaiting
delivery" rather than hiding the latency.

## Adding a demo for another framework

1. Create `examples/<name>-demo/` with its own `package.json` (private, `@hermes/<name>-demo`).
2. Proxy `/api` and `/v1` to `http://localhost:8899` in its dev server, so the browser sees one
   origin. `react-demo/vite.config.ts` is the model.
3. Render `<hermes-inbox>` with `api-url` set to the page's origin and `token-url="/api/session"`.
   That is genuinely all — the element handles the rest, and no framework-specific SDK is required.
4. Add the directory to `ci-web.yml`'s `examples` job. `pnpm-workspace.yaml` already globs
   `examples/*`.

The smallest possible version is a single `index.html` with a `<script type="module">` tag and no
build step at all, which is also the strongest evidence that the widget is genuinely
framework-agnostic.

## Testing

The demo is the subject of the live browser suite in [`tests/browser/`](../tests/browser). It drives a
real Chromium against a real cluster, and its centrepiece asserts that a notification arrives over the
websocket with **zero** inbox refetches.

```bash
make demo-e2e-install   # one-time: fetch the browser
make demo-e2e           # requires make dev-up
```
