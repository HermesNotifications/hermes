# ADR 0021: Create the first API key at install time and put it in a Secret

**Status:** Accepted  
**Date:** 2026-08-12  
**Author:** Daryl Robbins

---

## Context

Creating an API key requires an API key. `POST /v1/apikeys` demands `apikeys:manage`
(`internal/admin/handler_apikeys.go`), and `auth.APIKeyMiddleware` fronts every admin route with
no environment-variable backdoor — `SetSkipAuth` is reachable only from Go test code.

So a fresh installation had no way to obtain a first credential. `helm install` completed, every
pod reported Ready, `helm test` passed, and the operator could not make one authenticated call.
The only escape was to compute an HMAC-SHA256 by hand with `auth.HMACHashAPIKey`'s exact
semantics and `INSERT` a row with psql. That is not documented in `charts/hermes/templates/
NOTES.txt` or anywhere under `docs/self-hosting/`, and a `grep` of those four files for
"bootstrap" or "apikey" returned nothing.

`cmd/seed` exists but neither of its modes fits a cluster. `-env dev` writes
`web/admin/.env.local` on a developer's laptop. `-env staging|production` is hard-wired to AWS
Secrets Manager and imports the AWS SDK. Both are the wrong shape for a self-hoster on any
cluster, and the AWS one is the wrong shape for a published image.

This blocked every third-party install completely, which makes it the single largest obstacle
between this repository and something a stranger can use.

## Decision

We will ship `cmd/bootstrap`, published as `hermes-bootstrap`, and run it from the Helm chart as
an ordinary Job. It creates one API key with `auth.AllPermissions`, inserts the row, and writes
the raw key to a Kubernetes Secret named `<release>-hermes-bootstrap` under the key
`HERMES_API_KEY`.

It is idempotent across four paths, checked in order:

| Path | Condition | Behaviour |
|---|---|---|
| `supplied` | `bootstrap.existingSecret` set | Ensure the row only; never touch a Secret |
| `present` | Secret and row both exist | Nothing |
| `readopted` | Secret exists, row does not | Re-insert **the same** hash |
| `created` | Neither exists | Create both |

**Permissions are `AllPermissions`, not `DefaultPermissions`.** The latter omits
`apikeys:manage`, which would leave the bootstrap key unable to mint the narrower key meant to
replace it — the one thing it exists to do.

**No organization is created.** Under [ADR 0012](0012-api-keys-are-not-scoped-to-organizations.md)
an organization is a customer and the key carries no `organization_id`. Seeding a "Default"
organization would ship a fake customer, which is precisely the conflation that ADR prevents.
The first authenticated call is `GET /v1/organizations`, which correctly returns `[]`.

**The Secret is not tracked by Helm and carries no `ownerReference`.** `helm uninstall` leaves
it. This is the point of the `readopted` path: reinstalling against a surviving database
re-adopts the key the operator already saved rather than issuing a new one and silently
invalidating every place the old one was pasted. An `ownerReference` on the Job would be
garbage-collected at the next upgrade, which is the opposite of what is wanted.

**RBAC is the chart's first, and is scoped as narrowly as the API allows.** `create` on secrets
cannot take `resourceNames` — the object does not exist yet — so that verb is unavoidably broad.
`get` **is** restricted by `resourceNames` to the bootstrap Secret alone; without that the Role
would read every Secret in the namespace, including the one holding `HERMES_JWT_SECRET`. There
is no `update`, `delete`, `list` or `watch`: rotation is an operator action, and without `update`
a compromised bootstrap pod cannot overwrite the release's own Secret.

**The Kubernetes client is ~180 lines of `net/http`, not client-go.** The whole interaction is
one GET and one POST on one resource type, against which client-go would pull a large module
subtree into a repository with one indirect k8s dependency and thirteen binaries. The failure
modes it would buy are covered by design: a 409 is handled explicitly by adopting the winner's
Secret, retries are `backoffLimit: 6` (Kubernetes is the retry loop), and the ServiceAccount
token is read per request because projected tokens rotate on disk. `cmd/natskeys` sets the same
precedent, generating credentials outside the cluster rather than reaching for an API client.

## Consequences

- A clean install is usable. `kubectl get secret … | base64 -d` produces a working key, and
  `NOTES.txt` says so above everything else.
- The chart gains a ServiceAccount and a Role. Every other workload still runs as `default`,
  so this file is the one place RBAC has to be understood.
- `helm uninstall` leaves a Secret behind. Documented rather than fixed, for the reason above.
- `networkPolicy.enabled=true` needed a new egress rule. The existing policy allows 443 but not
  6443, so on k3s, kind, k3d and RKE — the clusters people evaluate on — the Job would have hung
  until its deadline with no useful error. `bootstrap.apiServerPort` exists for that, defaulting
  to 443.
- The bootstrap key is a root credential that lives until revoked. Its exposure is the same
  class as `<release>-secrets`, which already holds the HMAC secret and the JWT secret, so this
  adds no new tier — but it is worth saying out loud, and NOTES.txt does.
- One more image in both build matrices and in the ECR module.

## Alternatives considered

**Derive the key deterministically from `HERMES_API_KEY_HMAC_SECRET`.** Tempting: no RBAC, no
Secret, GitOps-pure, recomputable anywhere. Rejected because it changes what an HMAC-secret leak
means. Today that secret alone grants nothing — an attacker still needs a key's secret to
authenticate. Deriving the bootstrap key from it turns any leak of that one value into full
admin access.

**Generate the key in the Helm template with `randAlphaNum` and keep it stable with `lookup`.**
`lookup` returns empty under `helm template`, which is how Argo CD, Flux and `helm diff` render.
The key would churn on every sync. ADR 0008 shows this project already cares about those users.

**Extend `cmd/seed` with a third mode.** Rejected to keep the AWS SDK and the laptop-only
`writeAdminEnvLocal` out of a published image. The shared insert now lives in
`internal/apikeybootstrap` so the SQL exists once.

**Print the key to the Job's logs instead of a Secret.** No RBAC at all, and genuinely simpler.
Rejected as a default because Job pods are retained on purpose here (so `kubectl logs` works
after a failure), which means the raw key would persist in whatever collects logs, indefinitely,
in a system that has no other reason to hold credentials. `bootstrap.existingSecret` covers the
same "no Secret writes allowed" constraint without that.
