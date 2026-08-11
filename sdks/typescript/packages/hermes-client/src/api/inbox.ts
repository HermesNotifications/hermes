// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

  /**
   * Fetch just the unread count, without pulling any notifications.
   *
   * For a host that renders a bell badge with no inbox panel mounted. Cheap on the server (a
   * single cache read on a warm cache), but it is still an HTTP request and still subject to
   * the per-user rate limit — call it on mount and let the realtime channel keep it current,
   * rather than polling.
   */
  async unreadCount(): Promise<number> {
    const { data } = await this.send(() => this.client.GET("/v1/inbox/unread-count"));
    return data?.unread_count ?? 0;
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
