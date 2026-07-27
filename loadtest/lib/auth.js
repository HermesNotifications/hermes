// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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

export function jwtFor(userID, organizationID, opts) {
  const secret = __ENV.HERMES_JWT_SECRET || 'dev-jwt-secret';
  const now = Math.floor(Date.now() / 1000);
  const exp = now + (opts && opts.ttlSeconds ? opts.ttlSeconds : 3600);
  const header = { alg: 'HS256', typ: 'JWT' };
  const payload = {
    sub: userID,
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

export function userHeaders(userID, organizationID, extra) {
  const h = {
    'Authorization': `Bearer ${jwtFor(userID, organizationID)}`,
    'Content-Type': 'application/json',
    'X-Load-Test-Run-Id': __ENV.RUN_ID || 'local',
  };
  if (extra) Object.assign(h, extra);
  return h;
}
