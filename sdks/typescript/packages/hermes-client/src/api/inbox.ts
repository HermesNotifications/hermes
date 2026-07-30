// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "../generated/inbox-api.js";
import type { InboxPage } from "../types.js";
import { createSender, type ApiOptions } from "./request.js";

export type InboxAPIOptions = ApiOptions;

export class InboxAPI {
  private client: ReturnType<typeof createClient<paths>>;
  private send: ReturnType<typeof createSender>;

  constructor(baseUrl: string, getToken: () => string, options?: InboxAPIOptions) {
    const authMiddleware: Middleware = {
      async onRequest({ request }) {
        // Read the token per request rather than closing over it, so a refreshed token
        // takes effect on the very next call.
        request.headers.set("Authorization", `Bearer ${getToken()}`);
        return request;
      },
    };
    this.client = createClient<paths>({
      baseUrl,
      ...(options?.fetch ? { fetch: options.fetch } : {}),
    });
    this.client.use(authMiddleware);
    this.send = createSender("Inbox", options?.onUnauthorized);
  }

  async list(options?: {
    archived?: boolean;
    cursor?: string;
    limit?: number;
  }): Promise<InboxPage> {
    const { data } = await this.send(() =>
      this.client.GET("/v1/inbox", {
        params: {
          query: {
            archived: options?.archived,
            cursor: options?.cursor,
            limit: options?.limit,
          },
        },
      })
    );
    return {
      data: data?.data ?? [],
      unreadCount: data?.unread_count ?? 0,
      // The API sends "" on the last page; normalising to undefined is what makes
      // "there are no further pages" detectable downstream.
      cursor: data?.cursor || undefined,
    };
  }

  async markRead(id: string): Promise<void> {
    await this.send(() => this.client.PUT("/v1/inbox/{id}/read", { params: { path: { id } } }));
  }

  async markUnread(id: string): Promise<void> {
    await this.send(() =>
      this.client.DELETE("/v1/inbox/{id}/read", { params: { path: { id } } })
    );
  }

  async archive(id: string): Promise<void> {
    await this.send(() =>
      this.client.PUT("/v1/inbox/{id}/archive", { params: { path: { id } } })
    );
  }

  async unarchive(id: string): Promise<void> {
    await this.send(() =>
      this.client.DELETE("/v1/inbox/{id}/archive", { params: { path: { id } } })
    );
  }

  async delete(id: string): Promise<void> {
    await this.send(() => this.client.DELETE("/v1/inbox/{id}", { params: { path: { id } } }));
  }

  async markAllRead(): Promise<void> {
    await this.send(() => this.client.PUT("/v1/inbox/read-all"));
  }
}
