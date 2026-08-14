// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { SharedArray } from 'k6/data';

// Manifest is loaded once per k6 process into a SharedArray so N VUs share one copy.
// The manifest path is passed via env SEED_MANIFEST (default: loadtest/seed-manifest.json).
// open() resolves paths relative to this script's directory (loadtest/lib/).
// The default '../seed-manifest.json' therefore resolves to loadtest/seed-manifest.json
// from the repo root. Override with SEED_MANIFEST env var if needed.
const manifestPath = __ENV.SEED_MANIFEST || '../seed-manifest.json';

// The SharedArray holds only organization metadata -- id, index, user_count, categories.
// A handful of small objects, never the user list.
//
// Users are a pure function of (run_seed_id, organization index, i), mirroring UserID and
// ExternalID in cmd/loadseed/users.go, and are built one at a time by userAt() below.
//
// Two separate reasons this must not be a list. In the manifest, enumerating users cost
// ~110 bytes each, which at the seeder's own default of 100k users is a ~10MB file that the
// API server refuses to hold in a Secret. And in the VU, a SharedArray *deserialises a copy
// on every element access* -- so expanding the population into a module-scope array, which
// k6 evaluates per VU, gave every one of 500 VUs its own copy of all 20k users and
// OOM-killed the 8Gi runner pods before a single socket opened.
export const organizations = new SharedArray('organizations', function () {
  const m = JSON.parse(open(manifestPath));
  assertManifestShape(m);
  return m.organizations.map(function (o) {
    return { id: o.id, index: o.index, user_count: o.user_count, categories: o.categories };
  });
});

// totalUsers is the size of the seeded population across all organizations.
export const totalUsers = (function () {
  let n = 0;
  for (const o of organizations) n += o.user_count;
  return n;
})();

// userAt maps a flat index in [0, totalUsers) to its {organization, user} pair, allocating
// only the one pair asked for. This is the accessor the scenarios use in place of a
// materialised list of every (organization, user) combination.
export function userAt(index) {
  let i = ((index % totalUsers) + totalUsers) % totalUsers;
  for (const o of organizations) {
    if (i < o.user_count) {
      return {
        organization: o,
        user: { id: `lt-${runSeedID()}-t${o.index}-u${i}`, external_id: `ext-${o.index}-${i}` },
      };
    }
    i -= o.user_count;
  }
  throw new Error('userAt: index out of range: ' + index);
}

// assertManifestShape rejects a manifest written by a loadseed that predates this contract.
//
// Accepting one silently is the whole failure this guard exists for: an older manifest
// listed `users` as bare internal-id strings, the scenarios sent one as `to.user_id`,
// dispatch treated it as an unseen external id and created a second user, and the inbox
// push landed on that new user's channel while the VU listened on the original's. The run
// completes, the thresholds that matter are evaluated over zero samples, and it looks clean.
function assertManifestShape(m) {
  const orgs = m && m.organizations;
  if (!orgs || orgs.length === 0) throw new Error('seed manifest has no organizations');
  for (const o of orgs) {
    if (typeof o.user_count !== 'number' || typeof o.index !== 'number') {
      throw new Error(
        'seed manifest predates the derived-users change: every organization needs `index` ' +
        'and `user_count`, got ' + JSON.stringify(Object.keys(o)) +
        '. Re-run `make loadseed` to regenerate it.'
      );
    }
    if (o.user_count === 0) throw new Error('organization ' + o.id + ' has no users');
  }
  if (!m.run_seed_id) throw new Error('seed manifest has no run_seed_id');
}

export const manifest = new SharedArray('manifest_meta', function () {
  const raw = open(manifestPath);
  const m = JSON.parse(raw);
  return [{ api_key: m.api_key, run_seed_id: m.run_seed_id, seeded_at: m.seeded_at }];
});

export function apiKey() {
  return manifest[0].api_key;
}

export function runSeedID() {
  if (cachedRunSeedID === null) cachedRunSeedID = manifest[0].run_seed_id;
  return cachedRunSeedID;
}

// Cached per VU: every SharedArray element access deserialises, and userAt() is on the hot
// path of every send and every subscription.
let cachedRunSeedID = null;

// pickOrganization returns an organization object selected deterministically by VU+iter.
export function pickOrganization() {
  const idx = (__VU + __ITER) % organizations.length;
  return organizations[idx];
}

// pickUser returns a {id, external_id} pair. Use `external_id` for `to.user_id` on
// POST /v1/send, and `id` for the JWT `sub` and the `user#<id>` Centrifugo channel.
export function pickUser(organization) {
  const idx = (__VU * 31 + __ITER) % organization.user_count;
  return {
    id: `lt-${runSeedID()}-t${organization.index}-u${idx}`,
    external_id: `ext-${organization.index}-${idx}`,
  };
}

// pickTemplate walks organization.categories[*].subscriptions[*].templates[*]
// and selects one uniformly at random (per-iteration variance).
export function pickTemplate(organization) {
  const all = [];
  for (const c of organization.categories) {
    for (const s of c.subscriptions) {
      for (const t of s.templates) all.push(t);
    }
  }
  return all[(__VU * 17 + __ITER * 7) % all.length];
}

// There is deliberately no instanceRange() here any more.
//
// It sharded the user population using __ENV.INSTANCE_ID / __ENV.INSTANCE_COUNT, which
// k6-operator does not set: it passes those as k6 *tags* (--tag instance_id=1) and splits
// work with --execution-segment instead. So the function silently read unset variables,
// returned [0, total) on every pod, and sharded nothing -- while the scenarios also divided
// their VU counts by the same unset INSTANCE_COUNT. Splitting is k6's job via the segment;
// scenarios should treat the whole population as visible and let k6 decide who runs what.
