// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
