// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { LitElement, html, css, nothing, type TemplateResult } from "lit";
import { property } from "lit/decorators.js";
import {
  relativeTime,
  type HermesClient,
  type HermesError,
  type InboxState,
  type InboxUpdatedEvent,
  type NewNotificationEvent,
  type Notification,
  type RealtimeStatus,
} from "@hermes-notifications/client";
import { InboxController, type ClientFactory } from "./inbox-controller.js";

/** Schemes allowed to reach an `href`. Everything else is treated as no action at all. */
const SAFE_ACTION_PROTOCOLS = new Set(["http:", "https:"]);

/**
 * Resolve a notification's `action_url` to something safe to place in an `href`, or
 * `undefined` when it is not.
 *
 * `action_url` is attacker-influenced input: the send API accepts it as a free-form string
 * with no validation, and it also arrives over the websocket. lit does **not** sanitize
 * `href`, so an unchecked `javascript:` url would run script in the *host page* — in a
 * widget whose whole purpose is to be embedded in someone else's application.
 *
 * Relative urls are resolved against the document only to classify them; the original
 * string is returned so a single-page-app host that `preventDefault()`s and routes
 * internally still sees exactly what it was given.
 */
export function safeActionUrl(raw: string | undefined): string | undefined {
  if (raw === undefined) return undefined;
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;

  // The URL parser strips the tabs and newlines that "java\tscript:" style evasions rely
  // on, so delegating to it classifies those correctly rather than pattern-matching.
  const base = typeof document !== "undefined" ? document.baseURI : "https://example.invalid/";
  let parsed: URL;
  try {
    parsed = new URL(trimmed, base);
  } catch {
    return undefined;
  }
  return SAFE_ACTION_PROTOCOLS.has(parsed.protocol) ? trimmed : undefined;
}

/**
 * `<hermes-inbox>` — an embeddable notification inbox.
 *
 * Deliberately **not** decorated with `@customElement`: registration happens through
 * `registerHermesInbox()` or by importing `@hermes-notifications/web/define`, so that
 * importing this module is safe on a server. See `register.ts`.
 *
 * All state and network work lives in {@link InboxController}; this class is the view.
 *
 * ## Attributes
 * | Attribute | Purpose |
 * |---|---|
 * | `api-url` | Origin serving the inbox API. Defaults to the page's own origin. |
 * | `socket-url` | Base URL for the Centrifugo websocket. |
 * | `token` | A Hermes user JWT. Expires, so prefer `token-url`. |
 * | `token-url` | Endpoint on your app that mints a token; enables auto-refresh. |
 * | `user-id` | Internal Hermes user id. Defaults to the token's `sub` claim. |
 * | `page-size` | Rows per page (default 20). |
 * | `archived` | Show the archived view. |
 * | `open` | Reflected; whether the panel is open. |
 * | `heading` | Panel heading text. |
 * | `empty-text` | Text shown when there is nothing to list. |
 *
 * ## Events
 * All bubble and are composed, so a host can listen on an ancestor or on `document`.
 * `hermes-notification`, `hermes-update`, `hermes-unread-count-change`,
 * `hermes-open-change`, `hermes-connected`, `hermes-error`, `hermes-notification-click`,
 * and the cancellable `hermes-action`.
 */
export class HermesInbox extends LitElement {
  /**
   * Origin the inbox API is served from. Defaults to the page's own origin, which is the
   * right default when the host app proxies `/v1/*` — the common integration today, since
   * the services ship no CORS headers.
   */
  @property({ attribute: "api-url" }) apiUrl = "";
  @property({ attribute: "socket-url" }) socketUrl = "";
  @property() token = "";
  @property({ attribute: "token-url" }) tokenUrl = "";
  @property({ attribute: "user-id" }) userId = "";
  @property({ attribute: "page-size", type: Number }) pageSize = 20;
  @property({ type: Boolean }) archived = false;
  @property({ type: Boolean, reflect: true }) open = false;
  @property() heading = "Notifications";
  @property({ attribute: "empty-text" }) emptyText = "No notifications";

  /**
   * Supplies a fresh token on demand. A property rather than an attribute because a
   * callback cannot be expressed in HTML — `token-url` is the markup-only equivalent.
   */
  @property({ attribute: false }) getToken?: () => Promise<string>;

  /** A pre-built client. When set, the element does no token handling at all. */
  @property({ attribute: false }) client?: HermesClient;

  /** Overrides how the client is constructed. Escape hatch for tests and wrappers. */
  @property({ attribute: false }) clientFactory?: ClientFactory;

  private controller = new InboxController(this, {
    onNotification: (event: NewNotificationEvent) => this.emit("hermes-notification", event),
    onUpdate: (event: InboxUpdatedEvent) => this.emit("hermes-update", event),
    onUnreadCountChange: (count: number) => this.emit("hermes-unread-count-change", count),
    onStatusChange: (status: RealtimeStatus) => {
      // Every transition, so a host can show an accurate indicator. Reporting only the happy
      // path leaves a UI that latches to "connected" and never tells the truth again — and any
      // test gating on it silently stops gating.
      this.emit("hermes-realtime-change", { status });
      // Plus a dedicated signal for the common case: "publications will now reach me". Anything
      // waiting for the inbox to be live should use this rather than a timer, because a
      // publication landing before the channel subscription completes is lost.
      if (status === "connected") this.emit("hermes-connected", { status });
    },
    onError: (error: HermesError) => this.emit("hermes-error", error),
  });

  /** Read-only view of the inbox state, for hosts that want to render their own chrome. */
  get state(): Readonly<InboxState> {
    return this.controller.state;
  }

  private onDocumentPointerDown = (event: Event) => {
    // composedPath, not contains: across a shadow boundary the event target is retargeted
    // to the host, so `this.contains(event.target)` is true for every click on the page and
    // the panel would never close.
    if (!event.composedPath().includes(this)) this.setOpen(false);
  };

  private onDocumentKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape" && this.open) {
      this.setOpen(false);
      this.focusTrigger();
    }
  };

  static styles = css`
    :host {
      display: inline-block;
      position: relative;
      font-family: var(--hermes-font-family, system-ui, -apple-system, sans-serif);
      font-size: var(--hermes-font-size, 14px);
      color: var(--hermes-text-color, #1a1a1a);
    }

    button {
      font: inherit;
      color: inherit;
    }

    :focus-visible {
      outline: var(--hermes-focus-ring, 2px solid #3b82f6);
      outline-offset: 2px;
    }

    .visually-hidden {
      position: absolute;
      width: 1px;
      height: 1px;
      margin: -1px;
      padding: 0;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
      border: 0;
    }

    .trigger {
      position: relative;
      cursor: pointer;
      background: var(--hermes-trigger-bg, transparent);
      border: var(--hermes-trigger-border, 1px solid #e0e0e0);
      border-radius: var(--hermes-trigger-radius, 8px);
      padding: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: background 0.15s;
    }
    .trigger:hover {
      background: var(--hermes-trigger-hover-bg, #f5f5f5);
    }

    .badge {
      position: absolute;
      top: -4px;
      right: -4px;
      min-width: var(--hermes-badge-size, 18px);
      height: var(--hermes-badge-size, 18px);
      padding: 0 5px;
      border-radius: 9px;
      background: var(--hermes-badge-bg, #ef4444);
      color: var(--hermes-badge-color, #fff);
      font-size: 11px;
      font-weight: 600;
      display: flex;
      align-items: center;
      justify-content: center;
      line-height: 1;
    }

    .popover {
      position: absolute;
      top: calc(100% + 8px);
      right: 0;
      width: var(--hermes-popover-width, 380px);
      max-height: var(--hermes-popover-max-height, 480px);
      background: var(--hermes-popover-bg, #fff);
      border: 1px solid var(--hermes-border-color, #e0e0e0);
      border-radius: var(--hermes-popover-radius, 12px);
      box-shadow: var(--hermes-popover-shadow, 0 8px 30px rgba(0, 0, 0, 0.12));
      display: flex;
      flex-direction: column;
      overflow: hidden;
      /* A custom property, because a host with a modal above this value would otherwise
         clip the panel with no way to fix it from outside the shadow root. */
      z-index: var(--hermes-popover-z-index, 1000);
    }
    :host([data-placement="bottom-start"]) .popover {
      right: auto;
      left: 0;
    }

    .header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 14px 16px;
      border-bottom: 1px solid var(--hermes-border-color, #e0e0e0);
      font-weight: 600;
      font-size: 15px;
    }

    .mark-all-read {
      font-size: 12px;
      font-weight: 500;
      color: var(--hermes-accent-color, #3b82f6);
      cursor: pointer;
      background: none;
      border: none;
      padding: 4px 8px;
      border-radius: 4px;
    }
    .mark-all-read:hover {
      background: var(--hermes-accent-bg, #eff6ff);
    }

    .list {
      flex: 1;
      overflow-y: auto;
      overscroll-behavior: contain;
      max-height: var(--hermes-list-max-height, none);
    }

    .notification {
      display: flex;
      gap: 12px;
      padding: 12px 16px;
      border-bottom: 1px solid var(--hermes-border-color, #e0e0e0);
      transition: background 0.1s;
    }
    .notification:hover {
      background: var(--hermes-hover-bg, #f9f9f9);
    }
    .notification.unread {
      background: var(--hermes-unread-bg, #f0f7ff);
    }
    .notification.unread:hover {
      background: var(--hermes-unread-hover-bg, #e5f0ff);
    }

    .unread-dot {
      flex-shrink: 0;
      width: 8px;
      height: 8px;
      margin-top: 6px;
      border-radius: 50%;
      background: var(--hermes-accent-color, #3b82f6);
    }
    .read-dot {
      flex-shrink: 0;
      width: 8px;
      height: 8px;
      margin-top: 6px;
    }

    .notification-content {
      flex: 1;
      min-width: 0;
      text-align: left;
    }

    /* Rows are real buttons and links so they are keyboard reachable in the natural tab
       order. That needs the browser's own button styling stripped back. */
    .row-target {
      display: block;
      width: 100%;
      background: none;
      border: none;
      padding: 0;
      margin: 0;
      cursor: pointer;
      text-align: left;
      color: inherit;
      text-decoration: none;
    }

    .notification-title {
      font-weight: 500;
      margin-bottom: 2px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .notification-body {
      color: var(--hermes-muted-color, #666);
      font-size: 13px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .notification-time {
      color: var(--hermes-muted-color, #666);
      font-size: 11px;
      margin-top: 4px;
    }
    .action-link {
      color: var(--hermes-action-color, #3b82f6);
      font-size: 12px;
      font-weight: 500;
      display: inline-block;
      margin-top: 6px;
    }

    .actions {
      display: flex;
      gap: 4px;
      flex-shrink: 0;
      align-items: flex-start;
    }
    .action-btn {
      background: none;
      border: 1px solid var(--hermes-border-color, #e0e0e0);
      border-radius: 4px;
      padding: 4px 6px;
      cursor: pointer;
      font-size: 11px;
      color: var(--hermes-muted-color, #666);
    }
    .action-btn:hover {
      background: var(--hermes-hover-bg, #f5f5f5);
      color: var(--hermes-text-color, #1a1a1a);
    }

    .footer {
      padding: 8px;
      border-top: 1px solid var(--hermes-border-color, #e0e0e0);
    }
    .load-more {
      width: 100%;
      background: none;
      border: none;
      padding: 8px;
      cursor: pointer;
      color: var(--hermes-accent-color, #3b82f6);
      font-size: 13px;
      font-weight: 500;
      border-radius: 6px;
    }
    .load-more:hover {
      background: var(--hermes-accent-bg, #eff6ff);
    }
    .load-more[disabled] {
      cursor: default;
      color: var(--hermes-muted-color, #666);
    }

    .empty,
    .loading,
    .error {
      padding: 40px 16px;
      text-align: center;
      color: var(--hermes-muted-color, #666);
    }
    .loading {
      padding: 20px;
    }
    .error {
      padding: 20px 16px;
      color: var(--hermes-error-color, #b91c1c);
    }

    @media (prefers-reduced-motion: reduce) {
      .trigger,
      .notification {
        transition: none;
      }
    }
  `;

  connectedCallback(): void {
    super.connectedCallback();
    this.applyConfig();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.stopDismissListeners();
  }

  updated(): void {
    // Safe to call unconditionally: configure() compares against the applied config and
    // returns early when nothing changed. There is deliberately no watch list here — that
    // is what previously let `user-id` changes go unnoticed while double-applying the two
    // fields that *were* watched.
    this.applyConfig();
  }

  private applyConfig(): void {
    this.controller.configure({
      // Falling back to the page origin makes the same-origin/proxy setup — today's
      // supported integration — expressible without repeating the host name in markup.
      apiUrl: this.apiUrl || defaultOrigin(),
      ...(this.socketUrl ? { socketUrl: this.socketUrl } : {}),
      ...(this.token ? { token: this.token } : {}),
      ...(this.tokenUrl ? { tokenUrl: this.tokenUrl } : {}),
      ...(this.getToken ? { getToken: this.getToken } : {}),
      ...(this.userId ? { userId: this.userId } : {}),
      pageSize: this.pageSize,
      archived: this.archived,
      ...(this.client ? { client: this.client } : {}),
      ...(this.clientFactory ? { clientFactory: this.clientFactory } : {}),
    });
  }

  /** Dispatch a bubbling, composed CustomEvent. */
  private emit<T>(type: string, detail: T, cancelable = false): boolean {
    return this.dispatchEvent(
      // composed is what lets the event escape the shadow root at all; without it a React
      // wrapper — or any ancestor listener — never sees it.
      new CustomEvent<T>(type, { detail, bubbles: true, composed: true, cancelable })
    );
  }

  private setOpen(open: boolean): void {
    if (this.open === open) return;
    this.open = open;
    this.emit("hermes-open-change", { open });
    if (open) this.startDismissListeners();
    else this.stopDismissListeners();
  }

  private startDismissListeners(): void {
    document.addEventListener("pointerdown", this.onDocumentPointerDown, true);
    document.addEventListener("keydown", this.onDocumentKeyDown, true);
  }

  private stopDismissListeners(): void {
    document.removeEventListener("pointerdown", this.onDocumentPointerDown, true);
    document.removeEventListener("keydown", this.onDocumentKeyDown, true);
  }

  private focusTrigger(): void {
    this.renderRoot.querySelector<HTMLButtonElement>("button.trigger")?.focus();
  }

  private toggle(): void {
    this.setOpen(!this.open);
  }

  private onRowActivate(notification: Notification): void {
    this.emit("hermes-notification-click", { notification });
    if (!notification.read_at) void this.controller.markRead(notification.id);
  }

  private onActionActivate(event: Event, notification: Notification): void {
    // Cancellable so a single-page-app host can preventDefault() and route internally
    // rather than losing its state to a full page navigation.
    const proceed = this.emit("hermes-action", { notification }, true);
    if (!proceed) event.preventDefault();
    if (!notification.read_at) void this.controller.markRead(notification.id);
  }

  render(): TemplateResult {
    const { unreadCount } = this.controller.state;
    const label =
      unreadCount > 0 ? `${this.heading}, ${unreadCount} unread` : this.heading;

    return html`
      <button
        class="trigger"
        part="trigger"
        type="button"
        aria-label=${label}
        aria-haspopup="dialog"
        aria-expanded=${this.open ? "true" : "false"}
        aria-controls="hermes-panel"
        @click=${this.toggle}
      >
        ${renderBellIcon()}
        ${unreadCount > 0
          ? html`<span class="badge" part="badge" aria-hidden="true"
              >${unreadCount > 99 ? "99+" : unreadCount}</span
            >`
          : nothing}
      </button>

      <!-- Rendered unconditionally: a live region must already be in the DOM before its
           content changes, so putting aria-live on the conditional badge would announce
           nothing on the 0 -> 1 transition, which is the only one that matters. -->
      <span class="visually-hidden" part="status" role="status" aria-live="polite">
        ${unreadCount > 0 ? `${unreadCount} unread notifications` : ""}
      </span>

      ${this.open ? this.renderPanel() : nothing}
    `;
  }

  private renderPanel(): TemplateResult {
    const { notifications, unreadCount, loading, loadingMore, hasMore, error } =
      this.controller.state;

    return html`
      <div
        class="popover"
        part="popover"
        id="hermes-panel"
        role="dialog"
        aria-modal="false"
        aria-labelledby="hermes-heading"
      >
        <div class="header" part="header">
          <span id="hermes-heading">${this.heading}</span>
          ${unreadCount > 0
            ? html`<button
                class="mark-all-read"
                part="mark-all-read"
                type="button"
                @click=${() => void this.controller.markAllRead()}
              >
                Mark all read
              </button>`
            : nothing}
        </div>

        <div class="list" part="list">
          ${loading
            ? html`<div class="loading" part="loading">Loading…</div>`
            : notifications.length === 0
              ? html`<div class="empty" part="empty">${this.emptyText}</div>`
              : notifications.map((notification) => this.renderNotification(notification))}
          ${error && !loading
            ? html`<div class="error" part="error" role="alert">${error.message}</div>`
            : nothing}
        </div>

        ${hasMore
          ? html`<div class="footer" part="footer">
              <button
                class="load-more"
                part="load-more"
                type="button"
                ?disabled=${loadingMore}
                @click=${() => void this.controller.loadMore()}
              >
                ${loadingMore ? "Loading…" : "Load more"}
              </button>
            </div>`
          : nothing}
      </div>
    `;
  }

  private renderNotification(notification: Notification): TemplateResult {
    const isUnread = !notification.read_at;
    // A url that does not survive the scheme check is treated as no action: the row falls
    // back to a <button>, which still emits hermes-notification-click, and no action
    // affordance is shown for a link that would go nowhere.
    const actionUrl = safeActionUrl(notification.action_url);
    const hasAction = actionUrl !== undefined;

    const body = html`
      <div class="notification-title" part="title">${notification.title}</div>
      <div class="notification-body" part="body">${notification.body}</div>
      <div class="notification-time" part="time">
        ${relativeTime(notification.created_at)}
      </div>
      ${hasAction
        ? html`<span class="action-link" part="action-label"
            >${notification.action_label ?? "View"}</span
          >`
        : nothing}
    `;

    // An <a> when there is somewhere to go, a <button> otherwise — either way a real
    // interactive element, so the row is reachable by keyboard without any roving-tabindex
    // machinery. Previously rows were plain divs and entirely unreachable.
    const target = hasAction
      ? html`<a
          class="row-target"
          part="action-link"
          href=${actionUrl ?? "#"}
          @click=${(event: Event) => this.onActionActivate(event, notification)}
        >
          <div class="notification-content">${body}</div>
        </a>`
      : html`<button
          class="row-target"
          type="button"
          @click=${() => this.onRowActivate(notification)}
        >
          <div class="notification-content">${body}</div>
        </button>`;

    return html`
      <div
        class="notification ${isUnread ? "unread" : ""}"
        part="notification ${isUnread ? "unread" : "read"}"
      >
        ${isUnread
          ? html`<div class="unread-dot" part="unread-dot"></div>`
          : html`<div class="read-dot" part="read-dot"></div>`}
        ${target}
        <div class="actions" part="actions">
          ${isUnread
            ? html`<button
                class="action-btn"
                part="action-btn"
                type="button"
                aria-label="Mark ${notification.title} as read"
                @click=${() => void this.controller.markRead(notification.id)}
              >
                Read
              </button>`
            : nothing}
          <button
            class="action-btn"
            part="action-btn"
            type="button"
            aria-label="Archive ${notification.title}"
            @click=${() => void this.controller.archive(notification.id)}
          >
            Archive
          </button>
        </div>
      </div>
    `;
  }
}

function renderBellIcon(): TemplateResult {
  return html`<svg
    width="20"
    height="20"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
    <path d="M13.73 21a2 2 0 0 1-3.46 0" />
  </svg>`;
}

/** The page's own origin, or empty string where there is no document. */
function defaultOrigin(): string {
  return typeof location === "undefined" ? "" : location.origin;
}

declare global {
  interface HTMLElementTagNameMap {
    "hermes-inbox": HermesInbox;
  }
}
