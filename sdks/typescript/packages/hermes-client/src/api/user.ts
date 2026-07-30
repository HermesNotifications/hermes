// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "../generated/user-api.js";
import type { User, PreferenceCategory } from "../types.js";
import { createSender, type ApiOptions } from "./request.js";

export type UserAPIOptions = ApiOptions;

export class UserAPI {
  private client: ReturnType<typeof createClient<paths>>;
  private send: ReturnType<typeof createSender>;

  constructor(baseUrl: string, getToken: () => string, options?: UserAPIOptions) {
    const authMiddleware: Middleware = {
      async onRequest({ request }) {
        request.headers.set("Authorization", `Bearer ${getToken()}`);
        return request;
      },
    };
    this.client = createClient<paths>({
      baseUrl,
      ...(options?.fetch ? { fetch: options.fetch } : {}),
    });
    this.client.use(authMiddleware);
    this.send = createSender("User", options?.onUnauthorized);
  }

  async getProfile(): Promise<User> {
    const { data } = await this.send(() => this.client.GET("/v1/users/me"));
    return data as User;
  }

  /**
   * Replace the user's contact addresses.
   *
   * The wire shape is `{"contacts": {...}}` — nested, not flat. Keys are validated
   * server-side against the known address keys ("email", "phone").
   */
  async updateContacts(contacts: Record<string, string>): Promise<User> {
    const { data } = await this.send(() =>
      this.client.PUT("/v1/users/me/contacts", { body: { contacts } })
    );
    return data as User;
  }

  async getPreferenceCenter(): Promise<PreferenceCategory[]> {
    const { data } = await this.send(() => this.client.GET("/v1/users/me/preferences"));
    return data?.categories ?? [];
  }

  /**
   * Opt the user in or out of a subscription.
   *
   * A subscription whose category is `required` cannot be changed; the server answers
   * 403, which surfaces here as a `forbidden` HermesError.
   */
  async setPreference(subscriptionId: string, optedIn: boolean): Promise<void> {
    await this.send(() =>
      this.client.PUT("/v1/users/me/preferences/{subscription_id}", {
        params: { path: { subscription_id: subscriptionId } },
        body: { opted_in: optedIn },
      })
    );
  }

  /** Revert a preference to its category default. */
  async deletePreference(subscriptionId: string): Promise<void> {
    await this.send(() =>
      this.client.DELETE("/v1/users/me/preferences/{subscription_id}", {
        params: { path: { subscription_id: subscriptionId } },
      })
    );
  }
}
