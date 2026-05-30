// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { tenants, apiKey, pickTenant, pickUser, pickTemplate } from './seed.js';
import { adminHeaders, userHeaders, jwtFor } from './auth.js';

export const options = { vus: 1, iterations: 1 };

export default function () {
  if (!apiKey()) throw new Error('api_key missing');
  const t = pickTenant();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  const tok = jwtFor(u, t.id);
  if (tok.split('.').length !== 3) throw new Error('bad jwt');
  const ah = adminHeaders();
  const uh = userHeaders(u, t.id);
  if (!ah.Authorization.startsWith('Bearer ')) throw new Error('bad bearer');
  if (!uh.Authorization.startsWith('Bearer ')) throw new Error('bad user bearer');
  console.log(JSON.stringify({ tenant: t.id, user: u, template: tpl.id, jwtHead: tok.slice(0, 20) }));
}
