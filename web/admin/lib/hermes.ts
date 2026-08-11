// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { Hermes } from "@hermes-notifications/server";

let client: Hermes | null = null;

export function getHermes(): Hermes {
  if (!client) {
    const baseUrl = process.env.HERMES_API_URL;
    const apiKey = process.env.HERMES_API_KEY;

    if (!baseUrl || !apiKey) {
      throw new Error(
        "HERMES_API_URL and HERMES_API_KEY must be set"
      );
    }

    client = new Hermes({ baseUrl, apiKey });
  }
  return client;
}
