// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import encoding from 'k6/encoding';
import { organizations, apiKey, pickOrganization, pickUser, pickTemplate } from './seed.js';
import { adminHeaders, userHeaders, jwtFor } from './auth.js';
import { buildSendBody } from './payloads.js';

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

  assertIdentitySplit(u, t, tpl, tok);

  console.log(JSON.stringify({ organization: t.id, user: u, template: tpl.id, jwtHead: tok.slice(0, 20) }));
}

// assertIdentitySplit pins down which id goes where. The two ids are same-shaped strings
// that were silently interchangeable, and swapping them produced a run that passed every
// threshold while measuring nothing -- so the wiring is asserted rather than assumed.
function assertIdentitySplit(u, t, tpl, tok) {
  if (!u.id || !u.external_id) {
    throw new Error('manifest user missing an id half: ' + JSON.stringify(u));
  }
  if (u.id === u.external_id) {
    throw new Error('internal and external ids are identical, so this check proves nothing: ' + u.id);
  }

  // to.user_id must be the EXTERNAL id -- dispatch feeds it to EnsureUser(org, external_id).
  const body = buildSendBody(t, u, tpl);
  if (body.to.user_id !== u.external_id) {
    throw new Error(`to.user_id must be the external id ${u.external_id}, got ${body.to.user_id}`);
  }

  // The JWT subject must be the INTERNAL id -- Centrifugo derives user#<sub> from it.
  const claims = JSON.parse(decodeSegment(tok.split('.')[1]));
  if (claims.sub !== u.id) {
    throw new Error(`jwt sub must be the internal id ${u.id}, got ${claims.sub}`);
  }
  if (claims.organization_id !== t.id) {
    throw new Error(`jwt organization_id must be ${t.id}, got ${claims.organization_id}`);
  }

  // A bare id string must be refused rather than quietly interpreted as one half or the other.
  let refused = false;
  try { jwtFor(u.id, t.id); } catch (e) { refused = true; }
  if (!refused) throw new Error('jwtFor accepted a bare id string; the guard is not wired up');
}

// JWT segments are unpadded base64url, which is exactly k6's 'rawurl' encoding.
function decodeSegment(seg) {
  return encoding.b64decode(seg, 'rawurl', 's');
}
