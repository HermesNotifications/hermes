// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { apiKey } from './seed.js';

// HMAC-SHA256 JWT minting. Matches Hermes's inbox/user JWT verification
// (HS256 with HERMES_JWT_SECRET). Pure JS so it runs inside k6 with no external deps.
//
// Claim names confirmed from internal/auth/jwt_test.go and internal/auth/cached_keys_test.go:
//   UserIDClaim   = "sub"
//   OrganizationIDClaim = "organization_id"
function base64UrlEncode(s) {
  return encoding.b64encode(s, 'rawstd').replace(/\+/g, '-').replace(/\//g, '_');
}

// internalID pulls the internal id out of a manifest user, and refuses a bare string.
// A bare string is ambiguous -- it is either half of the identity, and picking the wrong
// one fails silently rather than loudly -- so it is rejected at the boundary.
export function internalID(user) {
  if (typeof user === 'string') {
    throw new Error(
      'expected a {id, external_id} user object, got the bare string ' + JSON.stringify(user) +
      '. Pass the manifest user through; use user.external_id only for to.user_id.'
    );
  }
  if (!user || !user.id) throw new Error('user has no internal id: ' + JSON.stringify(user));
  return user.id;
}

// jwtFor mints a token for a {id, external_id} user from the seed manifest.
//
// It takes the whole pair rather than an id string on purpose. `sub` must be the INTERNAL
// id -- Centrifugo derives the personal channel `user#<sub>` from it, and the inbox/user
// services resolve the row by it -- while `to.user_id` on a send must be the external id.
// Two same-shaped strings that must not be swapped is exactly how this went wrong before,
// so the pair travels together and each consumer picks its own half.
export function jwtFor(user, organizationID, opts) {
  const secret = __ENV.HERMES_JWT_SECRET || 'dev-jwt-secret';
  const now = Math.floor(Date.now() / 1000);
  const exp = now + (opts && opts.ttlSeconds ? opts.ttlSeconds : 3600);
  const header = { alg: 'HS256', typ: 'JWT' };
  const payload = {
    sub: internalID(user),
    organization_id: organizationID,
    iat: now,
    exp: exp,
  };
  const hb = base64UrlEncode(JSON.stringify(header));
  const pb = base64UrlEncode(JSON.stringify(payload));
  const signingInput = `${hb}.${pb}`;
  const sig = crypto.hmac('sha256', secret, signingInput, 'binary');
  const sb = encoding.b64encode(sig, 'rawstd').replace(/\+/g, '-').replace(/\//g, '_');
  return `${signingInput}.${sb}`;
}

export function adminHeaders(extra) {
  const h = {
    'Authorization': `Bearer ${apiKey()}`,
    'Content-Type': 'application/json',
    'X-Load-Test-Run-Id': __ENV.RUN_ID || 'local',
  };
  if (extra) Object.assign(h, extra);
  return h;
}

export function userHeaders(user, organizationID, extra) {
  const h = {
    'Authorization': `Bearer ${jwtFor(user, organizationID)}`,
    'Content-Type': 'application/json',
    'X-Load-Test-Run-Id': __ENV.RUN_ID || 'local',
  };
  if (extra) Object.assign(h, extra);
  return h;
}
