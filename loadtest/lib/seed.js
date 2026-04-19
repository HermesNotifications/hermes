import { SharedArray } from 'k6/data';

// Manifest is loaded once per k6 process into a SharedArray so N VUs share one copy.
// The manifest path is passed via env SEED_MANIFEST (default: loadtest/seed-manifest.json).
// open() resolves paths relative to this script's directory (loadtest/lib/).
// The default '../seed-manifest.json' therefore resolves to loadtest/seed-manifest.json
// from the repo root. Override with SEED_MANIFEST env var if needed.
const manifestPath = __ENV.SEED_MANIFEST || '../seed-manifest.json';

export const tenants = new SharedArray('tenants', function () {
  const raw = open(manifestPath);
  const m = JSON.parse(raw);
  return m.tenants;
});

export const manifest = new SharedArray('manifest_meta', function () {
  const raw = open(manifestPath);
  const m = JSON.parse(raw);
  return [{ api_key: m.api_key, run_seed_id: m.run_seed_id, seeded_at: m.seeded_at }];
});

export function apiKey() {
  return manifest[0].api_key;
}

export function runSeedID() {
  return manifest[0].run_seed_id;
}

// pickTenant returns a tenant object selected deterministically by VU+iter.
export function pickTenant() {
  const idx = (__VU + __ITER) % tenants.length;
  return tenants[idx];
}

export function pickUser(tenant) {
  const idx = (__VU * 31 + __ITER) % tenant.users.length;
  return tenant.users[idx];
}

// pickTemplate walks tenant.categories[*].subscriptions[*].templates[*]
// and selects one uniformly at random (per-iteration variance).
export function pickTemplate(tenant) {
  const all = [];
  for (const c of tenant.categories) {
    for (const s of c.subscriptions) {
      for (const t of s.templates) all.push(t);
    }
  }
  return all[(__VU * 17 + __ITER * 7) % all.length];
}

// instanceRange returns [start, end) user indices for THIS runner pod,
// based on env vars INSTANCE_ID and INSTANCE_COUNT injected by k6-operator.
// Used by the WS scenario so no two pods connect the same user.
export function instanceRange(totalCount) {
  const id = parseInt(__ENV.INSTANCE_ID || '0', 10);
  const n = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
  const per = Math.ceil(totalCount / n);
  return [id * per, Math.min((id + 1) * per, totalCount)];
}
