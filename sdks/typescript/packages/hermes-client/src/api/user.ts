// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "../generated/user-api.js";
import type { User, PreferenceCategory } from "../types.js";

export class UserAPI {
  private client: ReturnType<typeof createClient<paths>>;

  constructor(baseUrl: string, getToken: () => string) {
    const authMiddleware: Middleware = {
      async onRequest({ request }) {
        request.headers.set("Authorization", `Bearer ${getToken()}`);
        return request;
      },
    };
    this.client = createClient<paths>({ baseUrl });
    this.client.use(authMiddleware);
  }

  async getProfile(): Promise<User> {
    const { data, error, response } = await this.client.GET("/v1/users/me");
    if (error) throw new Error(`User API error (${response.status})`);
    return data;
  }

  async updateContacts(contacts: Record<string, string>): Promise<User> {
    const { data, error, response } = await this.client.PUT(
      "/v1/users/me/contacts",
      { body: { contacts } }
    );
    if (error) throw new Error(`User API error (${response.status})`);
    return data;
  }

  async getPreferenceCenter(): Promise<PreferenceCategory[]> {
    const { data, error, response } = await this.client.GET(
      "/v1/users/me/preferences"
    );
    if (error) throw new Error(`User API error (${response.status})`);
    return data.categories ?? [];
  }

  async setPreference(
    subscriptionId: string,
    optedIn: boolean
  ): Promise<void> {
    const { error, response } = await this.client.PUT(
      "/v1/users/me/preferences/{subscription_id}",
      {
        params: { path: { subscription_id: subscriptionId } },
        body: { opted_in: optedIn },
      }
    );
    if (error) throw new Error(`User API error (${response.status})`);
  }

  async deletePreference(subscriptionId: string): Promise<void> {
    const { error, response } = await this.client.DELETE(
      "/v1/users/me/preferences/{subscription_id}",
      { params: { path: { subscription_id: subscriptionId } } }
    );
    if (error) throw new Error(`User API error (${response.status})`);
  }
}
