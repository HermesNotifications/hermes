import { tenants, apiKey, pickTenant, pickUser, pickTemplate } from './seed.js';

export const options = { vus: 1, iterations: 1 };

export default function () {
  if (!apiKey()) throw new Error('api_key missing from manifest');
  if (tenants.length === 0) throw new Error('no tenants in manifest');
  const t = pickTenant();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  console.log(JSON.stringify({ tenant: t.id, user: u, template: tpl.id }));
}
