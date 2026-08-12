# ADR 0020: Own one spelling of the project's name, under the `HermesNotifications` org

**Status:** Accepted  
**Date:** 2026-08-12  
**Author:** Daryl Robbins

---

## Context

The project carried three spellings of its own name, and nothing forced them to agree:

| Where | Value |
|---|---|
| Git remote | `github.com/darylrobbins/hermes` |
| Go module (`go.mod`) | `github.com/hermes-notifications/hermes` |
| Chart default registry, all self-hosting docs | `ghcr.io/hermesnotifications` |

Finding 31.12 of the [2026-07-27 review](../reviews/2026-07-27-architecture-review.md) recorded
this and deferred it explicitly as "a decision, not an edit". It stayed deferred because nothing
had ever been published, so nothing was visibly broken.

Two things made it urgent at once. First, cutting the first release requires deciding where
artifacts go, and `secrets.GITHUB_TOKEN` in a `darylrobbins`-owned repository **cannot** write
packages into a `hermesnotifications` namespace — the publish step in `.github/workflows/cd.yml`
and all of `release-chart.yml` would have failed with 403 the first time a tag was pushed. Neither
workflow had ever run, so this had never been observed.

Second, the deferral was getting more expensive, not less. Every published artifact, every
`go get`, every chart pull would pin one of the three spellings and make the other two harder to
retire. The cheapest moment to fix an identity is before anything depends on it, and that moment
was closing.

A third spelling existed as a real GitHub org: both `hermes-notifications` (hyphenated, matching
the Go module and the npm scope) and `HermesNotifications` (unhyphenated, matching the chart and
docs) were owned and effectively empty.

## Decision

We will use **`HermesNotifications`** as the single owning identity:

- The repository is `github.com/HermesNotifications/hermes`, transferred from `darylrobbins`.
- The Go module is `github.com/hermesnotifications/hermes` — renamed, ~179 files, all internal
  imports.
- Images and charts publish to `ghcr.io/hermesnotifications`, which is what the chart's
  `global.image.registry` and the self-hosting docs already said.

GitHub owner names are case-insensitive in URLs, so the repository, the module path and the
registry namespace are now the same string.

The npm packages keep the hyphenated `@hermes-notifications` scope. npm and Go namespaces are
unrelated, the scope is already baked into published package metadata, and renaming it would
break consumers to buy nothing.

## Consequences

- The GHCR publish path works with the built-in `GITHUB_TOKEN`. No cross-org PAT, no GitHub App,
  no long-lived secret in Actions.
- `go install github.com/hermesnotifications/hermes/cmd/hermes@latest` — documented in
  `docs/cli.md` and **broken since it was written**, because the module path resolved to a
  repository that did not exist — starts working once `v0.1.0` is tagged.
- The AWS OIDC trust policy is scoped to `repo:<org>/<repo>:…`, so `infra/terraform/environments/
  {staging,production}.tfvars` must move to the new org and **be applied**. Until that apply
  happens, `cd.yml`'s ECR push fails authentication. This is the one consequence that requires an
  action outside this repository.
- ArgoCD `repoURL`s, Kargo stage configs and Prometheus `runbook_url` annotations were rewritten.
  GitHub redirects the old paths, so none of these were broken in the interim, but leaving them
  would have meant the identity was only settled on paper.
- Existing clones, forks and the `vps-gitops` references keep working through GitHub's permanent
  redirect. No consumer has to do anything.
- The hyphenated `hermes-notifications` org is now decorative. It should be left in place rather
  than deleted, so that nobody else can claim a name one hyphen away from ours.

## Alternatives considered

**Stay on `darylrobbins`, publish to `ghcr.io/darylrobbins`.** The least work today: the built-in
token already has the rights and no transfer is needed. Rejected because it settles the identity
on a personal namespace, which is the one option guaranteed to need a *second* rename if the
project is ever to read as a project rather than one person's repository. It also leaves the Go
module pointing at a nonexistent repository, so `docs/cli.md` stays broken.

**Transfer to the hyphenated `hermes-notifications` org.** Strictly less mechanical work — the Go
module would have been correct with zero code changes, and it matches the npm scope. It was
recommended on those grounds and not taken: the chart's registry default and the four self-hosting
documents already say `hermesnotifications`, and the unhyphenated spelling was judged the one to
keep. The cost of the choice is the 179-file module rename, paid once, in a commit that changes
nothing but import paths.

**Keep the repository on `darylrobbins` and push to the org with a PAT.** Rejected outright. It
requires a long-lived cross-org credential in Actions for a project that otherwise needs no
secrets beyond the built-in token, and it would have left all three spellings in place — solving
the publish failure while preserving the confusion that caused it.
