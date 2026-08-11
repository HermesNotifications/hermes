// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { organizations, apiKey, pickOrganization, pickUser, pickTemplate } from './seed.js';
import { adminHeaders, userHeaders, jwtFor } from './auth.js';

export const options = { vus: 1, iterations: 1 };

export default function () {
  if (!apiKey()) throw new Error('api_key missing');
  const t = pickOrganization();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  const tok = jwtFor(u, t.id);
  if (tok.split('.').length !== 3) throw new Error('bad jwt');
  const ah = adminHeaders();
  const uh = userHeaders(u, t.id);
  if (!ah.Authorization.startsWith('Bearer ')) throw new Error('bad bearer');
  if (!uh.Authorization.startsWith('Bearer ')) throw new Error('bad user bearer');
  console.log(JSON.stringify({ organization: t.id, user: u, template: tpl.id, jwtHead: tok.slice(0, 20) }));
}
