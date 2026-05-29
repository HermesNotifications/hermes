# Hermes Admin Portal

A Next.js admin UI for Hermes: manage subscription categories, subscriptions, and templates;
send notifications; inspect notification status and event timelines; and manage API keys.

## Stack

- **Next.js 15** (App Router) + **React 19** + **TypeScript**
- **Tailwind CSS 4** with **shadcn/ui** and **Base UI** primitives
- **better-auth** for authentication (backed by Postgres via `pg`)
- **react-hook-form** + **zod** for forms, **TanStack Table** for data grids,
  **sonner** for toasts, **lucide-react** for icons
- Talks to Hermes through the workspace SDK `@hermes-notifications/server`

## Prerequisites

- **pnpm** (the repo is a pnpm workspace; the portal depends on the local TS SDK)
- A running Hermes Admin API and Postgres — start the stack with `make dev-up`, or
  `make infra-up` + run the admin service (see [docs/development.md](../../docs/development.md))

## Setup

```bash
# from the repo root
make admin-install          # pnpm install in web/admin
# or directly:
cd web/admin && pnpm install
```

Create `.env.local` from the template:

```bash
cp web/admin/.env.example web/admin/.env.local
```

| Variable | Purpose |
|---|---|
| `HERMES_API_URL` | Base URL of the Hermes Admin API (e.g. `http://localhost:8080`) |
| `HERMES_API_KEY` | Admin API key the portal uses for server-to-server calls |
| `BETTER_AUTH_SECRET` | Secret for better-auth sessions (generate a random value) |
| `DATABASE_URL` | Postgres DSN for better-auth's tables |

> The better-auth tables are created by migration `000012` — run `make migrate` against the same
> database before first login.

## Run

```bash
make dev-admin              # next dev on http://localhost:3000
# or:
cd web/admin && pnpm dev
```

Other scripts (run from `web/admin/`): `pnpm build`, `pnpm start`, `pnpm lint`.

## Layout

```
web/admin/
  app/                  App Router routes
    (dashboard)/        Authenticated dashboard
    api/                Route handlers
    login/              Auth pages
    layout.tsx, page.tsx, globals.css
  components/           Domain + UI components
    ui/                 shadcn/ui primitives
    send-notification-form.tsx, template-form.tsx, category-form.tsx,
    subscription-list.tsx, notification-detail.tsx, event-timeline.tsx,
    notification-status-stepper.tsx, data-table.tsx, create-api-key-dialog.tsx, …
  hooks/                Custom React hooks
  lib/                  API clients & helpers
  middleware.ts         Auth middleware
  components.json       shadcn config
```

## Deployment

In production the portal ships as its own container image and is enabled via the Helm chart's
`adminPortal.*` values — see [self-hosting/configuration.md](../../docs/self-hosting/configuration.md).
