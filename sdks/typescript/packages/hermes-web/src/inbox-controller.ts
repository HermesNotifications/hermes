// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { ReactiveController, ReactiveControllerHost } from "lit";
import {
  HermesClient,
  HermesError,
  InboxStore,
  initialInboxState,
  type HermesClientConfig,
  type InboxState,
  type InboxUpdatedEvent,
  type NewNotificationEvent,
  type RealtimeStatus,
} from "@hermes-notifications/client";

/** Builds the client. Injected so the lifecycle can be driven without a socket. */
export type ClientFactory = (config: HermesClientConfig) => HermesClient;

/**
 * Everything that determines which inbox is shown and how.
 *
 * Every field is optional because callers hand over the *current* state of their inputs on
 * every render, which is frequently an incomplete picture — an element mid-upgrade, or a
 * React component whose token has not arrived yet. The controller decides when it has enough
 * to act, and does nothing until then.
 */
export interface InboxControllerConfig {
  apiUrl?: string;
  socketUrl?: string;
  /** A token supplied directly. Takes precedence over `tokenUrl`. */
  token?: string;
  /**
   * An endpoint on the host application that mints a token, returning
   * `{ token, expires_at }`. Fetched with credentials so the app's own session cookie
   * identifies the user.
   *
   * This is the only refresh mechanism a plain-HTML consumer can express, which makes it
   * load-bearing for the framework-agnostic case: with a static `token` attribute alone,
   * the widget stops working when that token expires.
   */
  tokenUrl?: string;
  /** Supplies a fresh token on demand. Takes precedence over `tokenUrl` for refresh. */
  getToken?: () => Promise<string>;
  /** Defaults to the token's `sub` claim, which is what the Centrifugo channel needs. */
  userId?: string;
  pageSize?: number;
  archived?: boolean;
  /** A pre-built client. When set, no token handling happens at all. */
  client?: HermesClient;
  /**
   * Overrides how the client is built, taking precedence over the factory given to the
   * constructor. This is what makes the element's `clientFactory` property mean something.
   *
   * Deliberately not part of {@link ClientIdentity}: identity is compared by value, and a
   * caller passing an inline arrow would then look different on every render and rebuild
   * forever. It is read when a client is next built, exactly like `getToken`.
   */
  clientFactory?: ClientFactory;
}

export interface InboxControllerOptions {
  clientFactory?: ClientFactory;
  /** Injected clock, so optimistic timestamps are exact in tests. */
  now?: () => string;
  /** Injected for tests; defaults to the global fetch. */
  fetch?: typeof fetch;
  onNotification?: (event: NewNotificationEvent) => void;
  onUpdate?: (event: InboxUpdatedEvent) => void;
  onUnreadCountChange?: (count: number) => void;
  onStatusChange?: (status: RealtimeStatus) => void;
  onError?: (error: HermesError) => void;
}

/** Seconds of margin before expiry at which a token-url token is renewed. */
const REFRESH_MARGIN_MS = 60_000;

/** The fields whose change requires a whole new client rather than just a reload. */
type ClientIdentity = {
  apiUrl?: string;
  socketUrl?: string;
  token?: string;
  tokenUrl?: string;
  userId?: string;
  pageSize?: number;
  archived?: boolean;
  hasInjectedClient: boolean;
};

/**
 * Owns the inbox lifecycle for a Lit host: resolving a token, building a client, driving an
 * {@link InboxStore}, and asking the host to re-render.
 *
 * The important design property is that {@link configure} is **convergent, not
 * event-driven**. Callers hand over the current value of every field whenever anything might
 * have changed; the controller compares that against what is already applied and does
 * nothing when they match. That removes two whole classes of bug at once: the element can
 * safely call it from both `connectedCallback` and `updated` without doing the work twice,
 * and there is no watch list to forget to extend when a new attribute is added.
 */
export class InboxController implements ReactiveController {
  private store?: InboxStore;
  private client?: HermesClient;
  /**
   * Whether {@link client} was built here. An injected client belongs to the caller, who may
   * be sharing it with other widgets, so tearing this controller down must not dispose it.
   */
  private ownsClient = false;
  private applied?: ClientIdentity;
  private storeUnsubscribe?: () => void;
  /** Unsubscribes for the handlers wired onto {@link client}, undone on every teardown. */
  private clientUnsubscribes: Array<() => void> = [];
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private currentToken = "";
  private snapshot: InboxState = initialInboxState;
  /** Guards against an in-flight configure being overtaken by a newer one. */
  private generation = 0;

  private readonly clientFactory: ClientFactory;
  private readonly now: () => string;
  private readonly fetchImpl: typeof fetch;

  constructor(
    private readonly host: ReactiveControllerHost,
    private readonly options: InboxControllerOptions = {}
  ) {
    this.clientFactory = options.clientFactory ?? ((config) => new HermesClient(config));
    this.now = options.now ?? (() => new Date().toISOString());
    this.fetchImpl = options.fetch ?? ((...args) => fetch(...args));
    host.addController(this);
  }

  get state(): InboxState {
    return this.snapshot;
  }

  hostConnected(): void {
    // Nothing to do: the host calls configure() with its current attribute values, and
    // configure() converges. Reconnecting after a move therefore rebuilds naturally.
  }

  hostDisconnected(): void {
    this.teardown();
    this.applied = undefined;
    this.snapshot = initialInboxState;
  }

  /**
   * Apply `config`, doing nothing if it matches what is already applied.
   *
   * Safe — and intended — to call on every render.
   */
  configure(config: InboxControllerConfig): void {
    const identity: ClientIdentity = {
      apiUrl: config.apiUrl,
      socketUrl: config.socketUrl,
      token: config.token,
      tokenUrl: config.tokenUrl,
      userId: config.userId,
      pageSize: config.pageSize,
      archived: config.archived,
      hasInjectedClient: config.client !== undefined,
    };

    if (this.applied && sameIdentity(this.applied, identity)) return;

    // An injected client is the caller's to manage; anything else needs a URL and some way
    // of getting a token before there is any point building one.
    const usable =
      config.client !== undefined ||
      (Boolean(config.apiUrl) && (Boolean(config.token) || Boolean(config.tokenUrl)));
    if (!usable) {
      // Going from a usable config to an unusable one is a real transition, not a no-op: a
      // host signing out drops its client and token, and leaving the previous store running
      // would keep the signed-out user's rows on screen and live-updating. Bumping the
      // generation also abandons any rebuild still in flight.
      if (this.applied) {
        this.generation++;
        this.teardown();
        this.applied = undefined;
        this.snapshot = initialInboxState;
        this.host.requestUpdate();
      }
      return;
    }

    this.applied = identity;
    void this.rebuild(config);
  }

  private async rebuild(config: InboxControllerConfig): Promise<void> {
    const generation = ++this.generation;
    this.teardown();

    let client: HermesClient;
    let ownsClient: boolean;
    if (config.client) {
      client = config.client;
      ownsClient = false;
    } else {
      let token = config.token ?? "";
      if (!token && config.tokenUrl) {
        const minted = await this.mintToken(config.tokenUrl);
        if (!minted) {
          // Forget the config rather than leaving it recorded as applied. teardown() has
          // already run, so there is no store and no client — and a config still marked
          // applied would short-circuit every later configure(), which the element fires on
          // every render. One transient failure of the token endpoint at mount would
          // otherwise strand the widget empty forever, with no path back.
          //
          // Only when still current: a newer configure() owns `applied` now, and clearing
          // it here would undo its bookkeeping.
          if (generation === this.generation) this.applied = undefined;
          return;
        }
        if (generation !== this.generation) return;
        token = minted.token;
        this.scheduleRefresh(config, minted.expiresAt);
      }
      this.currentToken = token;

      client = (config.clientFactory ?? this.clientFactory)({
        apiUrl: config.apiUrl ?? "",
        ...(config.socketUrl ? { socketUrl: config.socketUrl } : {}),
        token,
        ...(this.resolveGetToken(config) ? { getToken: this.resolveGetToken(config) } : {}),
      });
      ownsClient = true;
    }

    if (generation !== this.generation) {
      if (ownsClient) client.dispose?.();
      return;
    }

    this.client = client;
    this.ownsClient = ownsClient;
    this.wireClientEvents(client);

    const store = new InboxStore({
      client,
      ...(config.userId ? { userId: config.userId } : {}),
      ...(config.pageSize !== undefined ? { pageSize: config.pageSize } : {}),
      ...(config.archived !== undefined ? { archived: config.archived } : {}),
      now: this.now,
      // A shared client's socket is not ours to close on stop.
      ownsConnection: ownsClient,
    });
    this.store = store;
    this.storeUnsubscribe = store.subscribe(() => this.publish());

    await store.start();
    if (generation === this.generation) this.publish();
  }

  /**
   * Build the refresh callback handed to the client.
   *
   * An explicit `getToken` wins; otherwise a `tokenUrl` becomes one, so a socket reconnect
   * or a REST 401 mints a fresh token rather than failing.
   */
  private resolveGetToken(
    config: InboxControllerConfig
  ): (() => Promise<string>) | undefined {
    if (config.getToken) return config.getToken;
    if (!config.tokenUrl) return undefined;
    const tokenUrl = config.tokenUrl;
    return async () => {
      const minted = await this.mintToken(tokenUrl);
      if (!minted) return this.currentToken;
      this.currentToken = minted.token;
      this.scheduleRefresh(config, minted.expiresAt);
      return minted.token;
    };
  }

  private async mintToken(
    tokenUrl: string
  ): Promise<{ token: string; expiresAt?: string } | undefined> {
    try {
      const response = await this.fetchImpl(tokenUrl, {
        method: "GET",
        // The host app identifies the user by its own session cookie; the widget never
        // sees an API key.
        credentials: "include",
        headers: { Accept: "application/json" },
      });
      if (!response.ok) {
        throw HermesError.fromStatus("Token", response.status);
      }
      const body = (await response.json()) as { token?: unknown; expires_at?: unknown };
      if (typeof body.token !== "string" || body.token === "") {
        throw new HermesError("Token endpoint returned no token", "server");
      }
      return {
        token: body.token,
        ...(typeof body.expires_at === "string" ? { expiresAt: body.expires_at } : {}),
      };
    } catch (cause) {
      this.reportError(cause);
      return undefined;
    }
  }

  /** Renew proactively, a minute before expiry, so the socket never drops on a stale token. */
  private scheduleRefresh(config: InboxControllerConfig, expiresAt?: string): void {
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    if (!expiresAt || !config.tokenUrl) return;

    const expiry = new Date(expiresAt).getTime();
    if (Number.isNaN(expiry)) return;
    const delay = expiry - Date.now() - REFRESH_MARGIN_MS;
    if (delay <= 0) return;

    this.refreshTimer = setTimeout(() => {
      void this.resolveGetToken(config)?.().then((token) => {
        this.client?.setToken(token);
      });
    }, delay);
  }

  /**
   * Register the host's event forwarders, keeping each unsubscribe.
   *
   * Dropping them used to be survivable only because teardown disposed the client, which
   * cleared every handler wholesale. On a shared client — which is not ours to dispose —
   * the same handlers would otherwise pile up one set per rebuild, and the host would see
   * each notification duplicated once per rebuild it had ever done.
   */
  private wireClientEvents(client: HermesClient): void {
    const { onNotification, onUpdate, onUnreadCountChange, onStatusChange } = this.options;
    if (onNotification) this.clientUnsubscribes.push(client.on("notification", onNotification));
    if (onUpdate) this.clientUnsubscribes.push(client.on("update", onUpdate));
    if (onUnreadCountChange) {
      this.clientUnsubscribes.push(client.on("unreadCountChange", onUnreadCountChange));
    }
    if (onStatusChange) this.clientUnsubscribes.push(client.onStatusChange(onStatusChange));
  }

  /** Copy the store's snapshot out and re-render, reporting any newly recorded error. */
  private publish(): void {
    const next = this.store?.getSnapshot() ?? initialInboxState;
    if (next === this.snapshot) return;
    const previousError = this.snapshot.error;
    this.snapshot = next;
    if (next.error && next.error !== previousError) this.options.onError?.(next.error);
    this.host.requestUpdate();
  }

  private reportError(cause: unknown): void {
    const error =
      cause instanceof HermesError
        ? cause
        : new HermesError("Hermes inbox failed", "network", { cause });
    this.options.onError?.(error);
  }

  private teardown(): void {
    if (this.refreshTimer) {
      clearTimeout(this.refreshTimer);
      this.refreshTimer = undefined;
    }
    this.storeUnsubscribe?.();
    this.storeUnsubscribe = undefined;
    this.store?.dispose();
    this.store = undefined;

    for (const unsubscribe of this.clientUnsubscribes) unsubscribe();
    this.clientUnsubscribes = [];
    // Only a client we built. Disposing an injected one would clear every handler its
    // owner and any sibling widget registered, leaving them permanently deaf.
    if (this.ownsClient) this.client?.dispose();
    this.client = undefined;
    this.ownsClient = false;
  }

  refresh(): Promise<void> {
    return this.store?.refresh() ?? Promise.resolve();
  }
  loadMore(): Promise<void> {
    return this.store?.loadMore() ?? Promise.resolve();
  }
  markRead(id: string): Promise<void> {
    return this.store?.markRead(id) ?? Promise.resolve();
  }
  markUnread(id: string): Promise<void> {
    return this.store?.markUnread(id) ?? Promise.resolve();
  }
  archive(id: string): Promise<void> {
    return this.store?.archive(id) ?? Promise.resolve();
  }
  unarchive(id: string): Promise<void> {
    return this.store?.unarchive(id) ?? Promise.resolve();
  }
  remove(id: string): Promise<void> {
    return this.store?.remove(id) ?? Promise.resolve();
  }
  markAllRead(): Promise<void> {
    return this.store?.markAllRead() ?? Promise.resolve();
  }
  clearError(): void {
    this.store?.clearError();
  }
}

function sameIdentity(a: ClientIdentity, b: ClientIdentity): boolean {
  return (
    a.apiUrl === b.apiUrl &&
    a.socketUrl === b.socketUrl &&
    a.token === b.token &&
    a.tokenUrl === b.tokenUrl &&
    a.userId === b.userId &&
    a.pageSize === b.pageSize &&
    a.archived === b.archived &&
    a.hasInjectedClient === b.hasInjectedClient
  );
}
