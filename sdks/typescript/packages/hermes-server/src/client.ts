import createClient, { type Middleware } from "openapi-fetch";
import type { paths, components } from "./generated/admin-api.js";

export type NotificationGroup = components["schemas"]["NotificationGroup"];
export type NotificationType = components["schemas"]["NotificationType"];
export type Notification = components["schemas"]["Notification"];
export type NotificationEvent = components["schemas"]["NotificationEvent"];

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

export class GroupsService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<NotificationGroup[]> {
    const result = await this.client.GET("/v1/groups");
    return unwrap(result) ?? [];
  }

  async create(body: {
    slug: string;
    name: string;
    defaultChannels?: string[];
  }): Promise<NotificationGroup> {
    const result = await this.client.POST("/v1/groups", {
      body: { slug: body.slug, name: body.name, default_channels: body.defaultChannels ?? null },
    });
    return unwrap(result);
  }

  async update(
    id: string,
    body: { name?: string; defaultChannels?: string[] }
  ): Promise<NotificationGroup> {
    const result = await this.client.PUT("/v1/groups/{id}", {
      params: { path: { id } },
      body: { name: body.name ?? "", default_channels: body.defaultChannels ?? null },
    });
    return unwrap(result);
  }
}

export class TypesService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async list(): Promise<NotificationType[]> {
    const result = await this.client.GET("/v1/types");
    return unwrap(result) ?? [];
  }

  async create(body: {
    groupId: string;
    slug: string;
    name: string;
    emailSubject?: string;
    emailBody?: string;
    smsBody?: string;
    inboxTitle?: string;
    inboxBody?: string;
  }): Promise<NotificationType> {
    const result = await this.client.POST("/v1/types", {
      body: {
        group_id: body.groupId,
        slug: body.slug,
        name: body.name,
        email_subject: body.emailSubject,
        email_body: body.emailBody,
        sms_body: body.smsBody,
        inbox_title: body.inboxTitle,
        inbox_body: body.inboxBody,
      },
    });
    return unwrap(result);
  }

  async update(
    id: string,
    body: {
      name?: string;
      emailSubject?: string;
      emailBody?: string;
      smsBody?: string;
      inboxTitle?: string;
      inboxBody?: string;
    }
  ): Promise<NotificationType> {
    const result = await this.client.PUT("/v1/types/{id}", {
      params: { path: { id } },
      body: {
        name: body.name ?? "",
        email_subject: body.emailSubject,
        email_body: body.emailBody,
        sms_body: body.smsBody,
        inbox_title: body.inboxTitle,
        inbox_body: body.inboxBody,
      },
    });
    return unwrap(result);
  }

  async delete(id: string): Promise<void> {
    const result = await this.client.DELETE("/v1/types/{id}", {
      params: { path: { id } },
    });
    unwrap(result);
  }
}

export interface SendOptions {
  tenantId: string;
  userId: string;
  type?: string;
  content?: { title: string; body: string; actionUrl?: string; actionLabel?: string };
  data?: Record<string, unknown>;
  channels?: string[];
  group?: string;
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
        tenant_id: options.tenantId,
        user_id: options.userId,
        type: options.type,
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
        group: options.group,
      },
    });
    const data = unwrap(result);
    return { notificationId: data.notification_id };
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

export class AuthService {
  constructor(private client: ReturnType<typeof createApiClient>) {}

  async exchangeToken(options: {
    userId: string;
    tenantId: string;
  }): Promise<{ token: string; expiresAt: string }> {
    const result = await this.client.POST("/v1/auth/token", {
      body: {
        user_id: options.userId,
        tenant_id: options.tenantId,
      },
    });
    const data = unwrap(result);
    return { token: data.token, expiresAt: data.expires_at };
  }
}

export class Hermes {
  readonly groups: GroupsService;
  readonly types: TypesService;
  readonly notifications: NotificationsService;
  readonly auth: AuthService;

  constructor(config: HermesConfig) {
    const client = createApiClient(config);
    this.groups = new GroupsService(client);
    this.types = new TypesService(client);
    this.notifications = new NotificationsService(client);
    this.auth = new AuthService(client);
  }
}
