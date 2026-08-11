// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import createClient, { type Middleware } from "openapi-fetch";
import type { paths, components } from "./generated/admin-api.js";

export type SubscriptionCategory = components["schemas"]["SubscriptionCategory"];
export type Subscription = components["schemas"]["Subscription"];
export type NotificationTemplate = components["schemas"]["NotificationTemplate"];
export type Notification = components["schemas"]["Notification"];
export type NotificationEvent = components["schemas"]["NotificationEvent"];
export type NotificationItem = components["schemas"]["NotificationItem"];
export type Organization = components["schemas"]["OrganizationItem"];
export type User = components["schemas"]["UserItem"];

export interface HermesConfig {
  baseUrl: string;
  apiKey: string;
  fetch?: typeof globalThis.fetch;
}

export class HermesError extends Error {
  status: number;
  detail: string;

  constructor(status: number, detail: string) {
    super(`Hermes API error (${status}): ${detail}`);
    this.name = "HermesError";
    this.status = status;
    this.detail = detail;
  }
}

function createApiClient(config: HermesConfig) {
  const authMiddleware: Middleware = {
    async onRequest({ request }) {
      request.headers.set("Authorization", `Bearer ${config.apiKey}`);
      return request;
    },
  };

  const client = createClient<paths>({
    baseUrl: config.baseUrl,
    fetch: config.fetch,
  });
  client.use(authMiddleware);
  return client;
}

function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.error) {
    const err = result.error as { status?: number; detail?: string };
    throw new HermesError(
      result.response.status,
      err.detail ?? result.response.statusText
    );
  }
  return result.data as T;
}

export class CategoriesService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<SubscriptionCategory[]> {
    const result = await this.client.GET("/v1/subscriptions/categories");
    return unwrap(result) ?? [];
  }

  async create(body: {
    slug: string;
    name: string;
    defaultChannels?: string[];
    defaultState?: "on" | "off" | "required";
    sortOrder?: number;
  }): Promise<SubscriptionCategory> {
    const result = await this.client.POST("/v1/subscriptions/categories", {
      body: {
        slug: body.slug,
        name: body.name,
        default_channels: body.defaultChannels ?? null,
        default_state: body.defaultState ?? "on",
        sort_order: body.sortOrder ?? 0,
      },
    });
    return unwrap(result);
  }

  async update(
    id: string,
    body: {
      name?: string;
      defaultChannels?: string[];
      defaultState?: "on" | "off" | "required";
      sortOrder?: number;
    }
  ): Promise<SubscriptionCategory> {
    const result = await this.client.PUT("/v1/subscriptions/categories/{id}", {
      params: { path: { id } },
      body: {
        name: body.name ?? "",
        default_channels: body.defaultChannels ?? null,
        default_state: body.defaultState ?? "on",
        sort_order: body.sortOrder ?? 0,
      },
    });
    return unwrap(result);
  }

  async delete(id: string): Promise<void> {
    const result = await this.client.DELETE("/v1/subscriptions/categories/{id}", {
      params: { path: { id } },
    });
    unwrap(result);
  }
}

export class SubscriptionsService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(categoryId: string): Promise<Subscription[]> {
    const result = await this.client.GET(
      "/v1/subscriptions/categories/{category_id}/subscriptions",
      { params: { path: { category_id: categoryId } } }
    );
    return unwrap(result) ?? [];
  }

  async create(
    categoryId: string,
    body: { slug: string; name: string; sortOrder?: number }
  ): Promise<Subscription> {
    const result = await this.client.POST(
      "/v1/subscriptions/categories/{category_id}/subscriptions",
      {
        params: { path: { category_id: categoryId } },
        body: {
          slug: body.slug,
          name: body.name,
          sort_order: body.sortOrder ?? 0,
        },
      }
    );
    return unwrap(result);
  }

  async update(
    id: string,
    body: { name?: string; sortOrder?: number }
  ): Promise<Subscription> {
    const result = await this.client.PUT("/v1/subscriptions/{id}", {
      params: { path: { id } },
      body: { name: body.name ?? "", sort_order: body.sortOrder ?? 0 },
    });
    return unwrap(result);
  }

  async delete(id: string): Promise<void> {
    const result = await this.client.DELETE("/v1/subscriptions/{id}", {
      params: { path: { id } },
    });
    unwrap(result);
  }
}

export class TemplatesService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<NotificationTemplate[]> {
    const result = await this.client.GET("/v1/templates");
    return unwrap(result) ?? [];
  }

  async create(body: {
    slug: string;
    name: string;
    subscriptionId?: string;
    defaultChannels?: string[];
    content?: Record<string, Record<string, string>>;
  }): Promise<NotificationTemplate> {
    const result = await this.client.POST("/v1/templates", {
      body: {
        slug: body.slug,
        name: body.name,
        subscription_id: body.subscriptionId,
        default_channels: body.defaultChannels ?? null,
        content: body.content,
      },
    });
    return unwrap(result);
  }

  async update(
    id: string,
    body: {
      name?: string;
      defaultChannels?: string[];
      content?: Record<string, Record<string, string>>;
    }
  ): Promise<NotificationTemplate> {
    const result = await this.client.PUT("/v1/templates/{id}", {
      params: { path: { id } },
      body: {
        name: body.name ?? "",
        default_channels: body.defaultChannels ?? null,
        content: body.content,
      },
    });
    return unwrap(result);
  }

  async delete(id: string): Promise<void> {
    const result = await this.client.DELETE("/v1/templates/{id}", {
      params: { path: { id } },
    });
    unwrap(result);
  }
}

export interface SendOptions {
  to: {
    organizationId: string;
    userId: string;
    contacts?: Record<string, string>;
  };
  template?: string;
  content?: {
    title: string;
    body: string;
    actionUrl?: string;
    actionLabel?: string;
  };
  data?: Record<string, unknown>;
  channels?: string[];
  idempotencyKey?: string;
}

export class NotificationsService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async send(options: SendOptions): Promise<{ notificationId: string }> {
    const result = await this.client.POST("/v1/send", {
      params: {
        header: options.idempotencyKey
          ? { "X-Idempotency-Key": options.idempotencyKey }
          : undefined,
      },
      body: {
        to: {
          organization_id: options.to.organizationId,
          user_id: options.to.userId,
          contacts: options.to.contacts,
        },
        template: options.template,
        content: options.content
          ? {
              title: options.content.title,
              body: options.content.body,
              action_url: options.content.actionUrl,
              action_label: options.content.actionLabel,
            }
          : undefined,
        data: options.data,
        channels: options.channels,
      },
    });
    const data = unwrap(result);
    return { notificationId: data.notification_id };
  }

  async list(limit?: number): Promise<NotificationItem[]> {
    const result = await this.client.GET("/v1/notifications", {
      params: { query: limit ? { limit } : {} },
    });
    return unwrap(result) ?? [];
  }

  async getStatus(id: string): Promise<{
    notification: Notification;
    events: NotificationEvent[];
  }> {
    const result = await this.client.GET("/v1/notifications/{id}", {
      params: { path: { id } },
    });
    const data = unwrap(result);
    return { notification: data.notification, events: data.events ?? [] };
  }
}

export type APIKeyInfo = {
  id: string;
  name: string;
  permissions: string[] | null;
  created_at: string;
};

export type APIKeyCreated = APIKeyInfo & {
  raw_key: string;
};

export class APIKeysService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<APIKeyInfo[]> {
    const result = await this.client.GET("/v1/apikeys");
    return unwrap(result) ?? [];
  }

  async create(body: {
    name: string;
    permissions?: string[];
  }): Promise<APIKeyCreated> {
    const result = await this.client.POST("/v1/apikeys", {
      body: {
        name: body.name,
        permissions: body.permissions,
      },
    });
    return unwrap(result);
  }

  async delete(id: string): Promise<void> {
    const result = await this.client.DELETE("/v1/apikeys/{id}", {
      params: { path: { id } },
    });
    unwrap(result);
  }
}

export class AuthService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async exchangeToken(options: {
    userId: string;
    organizationId: string;
  }): Promise<{ token: string; expiresAt: string }> {
    const result = await this.client.POST("/v1/auth/token", {
      body: {
        user_id: options.userId,
        organization_id: options.organizationId,
      },
    });
    const data = unwrap(result);
    return { token: data.token, expiresAt: data.expires_at };
  }
}

export class OrganizationsService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<Organization[]> {
    const result = await this.client.GET("/v1/organizations");
    return unwrap(result) ?? [];
  }

  async create(body: { name: string }): Promise<Organization> {
    const result = await this.client.POST("/v1/organizations", {
      body: { name: body.name },
    });
    return unwrap(result);
  }
}

export class UsersService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(organizationId?: string): Promise<User[]> {
    const result = await this.client.GET("/v1/users", {
      params: { query: organizationId ? { organization_id: organizationId } : {} },
    });
    return unwrap(result) ?? [];
  }
}

export class Hermes {
  readonly categories: CategoriesService;
  readonly subscriptions: SubscriptionsService;
  readonly templates: TemplatesService;
  readonly notifications: NotificationsService;
  readonly auth: AuthService;
  readonly apiKeys: APIKeysService;
  readonly organizations: OrganizationsService;
  readonly users: UsersService;

  constructor(config: HermesConfig) {
    const client = createApiClient(config);
    this.categories = new CategoriesService(client);
    this.subscriptions = new SubscriptionsService(client);
    this.templates = new TemplatesService(client);
    this.notifications = new NotificationsService(client);
    this.auth = new AuthService(client);
    this.apiKeys = new APIKeysService(client);
    this.organizations = new OrganizationsService(client);
    this.users = new UsersService(client);
  }
}
