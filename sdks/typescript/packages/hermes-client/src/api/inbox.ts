// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "../generated/inbox-api.js";
import type { InboxPage } from "../types.js";

export class InboxAPI {
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

  async list(options?: {
    archived?: boolean;
    cursor?: string;
    limit?: number;
  }): Promise<InboxPage> {
    const { data, error, response } = await this.client.GET("/v1/inbox", {
      params: {
        query: {
          archived: options?.archived,
          cursor: options?.cursor,
          limit: options?.limit,
        },
      },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
    return {
      data: data.data ?? [],
      unreadCount: data.unread_count,
      cursor: data.cursor || undefined,
    };
  }

  async markRead(id: string): Promise<void> {
    const { error, response } = await this.client.PUT("/v1/inbox/{id}/read", {
      params: { path: { id } },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }

  async markUnread(id: string): Promise<void> {
    const { error, response } = await this.client.DELETE("/v1/inbox/{id}/read", {
      params: { path: { id } },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }

  async archive(id: string): Promise<void> {
    const { error, response } = await this.client.PUT("/v1/inbox/{id}/archive", {
      params: { path: { id } },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }

  async unarchive(id: string): Promise<void> {
    const { error, response } = await this.client.DELETE("/v1/inbox/{id}/archive", {
      params: { path: { id } },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }

  async delete(id: string): Promise<void> {
    const { error, response } = await this.client.DELETE("/v1/inbox/{id}", {
      params: { path: { id } },
    });
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }

  async markAllRead(): Promise<void> {
    const { error, response } = await this.client.PUT("/v1/inbox/read-all");
    if (error) throw new Error(`Inbox API error (${response.status})`);
  }
}
