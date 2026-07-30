// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { subjectFromToken } from "@hermes-notifications/client";

/**
 * Who the demo is acting as.
 *
 * In a real integration this comes from your own session — a cookie, a JWT, whatever you
 * already use. It must never come from a request parameter, or anyone can mint a token for
 * anyone.
 */
export interface Identity {
  /** Your Hermes organization id. Note this column is a UUID. */
  organizationId: string;
  /** Your own user identifier. Hermes maps it to an internal id on first use. */
  externalUserId: string;
}

/**
 * The slice of the admin SDK needed to mint a token, as an interface so this module can be
 * tested without an HTTP server, and so the dependency is visible.
 */
export interface TokenMinter {
  exchangeToken(options: {
    userId: string;
    organizationId: string;
  }): Promise<{ token: string; expiresAt: string }>;
}

/** What the browser receives. Contains no credential beyond the short-lived user token. */
export interface SessionPayload {
  token: string;
  expiresAt: string;
  /**
   * The internal Hermes user id, decoded from the token's `sub` claim.
   *
   * The browser needs this for the Centrifugo channel (`user#<sub>`) and has no other way to
   * get it. Returning it here is not a convenience: guessing the external id instead produces a
   * subscription Centrifugo rejects while REST keeps working, so the inbox loads normally and
   * then silently never updates — the hardest failure of this integration to diagnose.
   */
  hermesUserId: string;
  externalUserId: string;
  organizationId: string;
  /** Where the browser should open its websocket. Cross-origin, and that is fine. */
  socketUrl: string;
}

/**
 * Mint a user token for `identity` and assemble what the browser needs.
 *
 * This is the only place the Hermes API key is used. Copy this shape: derive the identity from
 * your own server-side session, never from the request body.
 */
export async function createSession(
  hermes: TokenMinter,
  identity: Identity,
  options: { socketUrl: string }
): Promise<SessionPayload> {
  const { token, expiresAt } = await hermes.exchangeToken({
    userId: identity.externalUserId,
    organizationId: identity.organizationId,
  });

  const hermesUserId = subjectFromToken(token);
  if (!hermesUserId) {
    // Fail loudly. A session without `sub` yields a browser subscribing to a channel nobody
    // publishes to, which looks like "realtime is broken" rather than "the token is wrong".
    throw new Error(
      "Hermes returned a token with no `sub` claim; cannot determine the realtime channel"
    );
  }

  return {
    token,
    expiresAt,
    hermesUserId,
    externalUserId: identity.externalUserId,
    organizationId: identity.organizationId,
    socketUrl: options.socketUrl,
  };
}
