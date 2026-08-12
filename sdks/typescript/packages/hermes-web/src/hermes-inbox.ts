// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { LitElement, html, css, nothing, type TemplateResult } from "lit";
import { property, state } from "lit/decorators.js";
import {
  notificationLevel,
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

/** The subset of an element's box metrics that decides whether its text is clipped. */
export interface TruncationBox {
  scrollHeight: number;
  clientHeight: number;
  scrollWidth: number;
  clientWidth: number;
}

/**
 * Whether either dimension of a box is clipping its content.
 *
 * Extracted and exported because it is the one part of the overflow probe that can be tested
 * without a layout engine: jsdom reports every one of these as 0, so a test there can only
 * assert the arithmetic, never the measurement.
 *
 * The one-pixel tolerance is not defensive padding. Line boxes land on fractional pixels at
 * non-integer zoom and with some fonts, so `scrollHeight` exceeds `clientHeight` by a fraction
 * on rows that are visually not clipped at all — and a toggle that appears and disappears as
 * the user zooms is worse than no toggle.
 */
export function isTruncated(box: TruncationBox): boolean {
  return box.scrollHeight > box.clientHeight + 1 || box.scrollWidth > box.clientWidth + 1;
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
  @property({ attribute: "expand-text" }) expandText = "Show more";
  @property({ attribute: "collapse-text" }) collapseText = "Show less";

  /**
   * Supplies a fresh token on demand. A property rather than an attribute because a
   * callback cannot be expressed in HTML — `token-url` is the markup-only equivalent.
   */
  @property({ attribute: false }) getToken?: () => Promise<string>;

  /** A pre-built client. When set, the element does no token handling at all. */
  @property({ attribute: false }) client?: HermesClient;

  /** Overrides how the client is constructed. Escape hatch for tests and wrappers. */
  @property({ attribute: false }) clientFactory?: ClientFactory;

  /**
   * Overrides how a row is judged to be clipped. Escape hatch for tests, mirroring
   * `clientFactory`.
   *
   * The real probe reads layout, and jsdom has none — every box there measures zero, so the
   * toggle would never render and the whole behaviour would be assertable only in a browser.
   * Injecting the predicate lets the unit suite cover the markup, the ARIA wiring and the
   * click semantics, and leaves Playwright to assert the one thing that genuinely needs a
   * layout engine: that the clamp lifts.
   */
  @property({ attribute: false }) overflowProbe?: (row: {
    id: string;
    title: HTMLElement;
    body: HTMLElement;
  }) => boolean;

  /** Rows the user has expanded, by notification id. */
  @state() private expanded = new Set<string>();

  /** Rows whose text is clipped and therefore need a toggle, by notification id. */
  @state() private overflowing = new Set<string>();

  /** Watches row bodies for reflow. Absent in jsdom, and lazily created for that reason. */
  private resizeObserver?: ResizeObserver;

  /** Coalesces observer callbacks into one re-measure per frame. */
  private remeasureHandle?: number;

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

    /*
     * Borderless by default: a bell in a box reads as a form control sitting in someone's
     * header, not as an icon button.
     *
     * The default is a *transparent* border rather than \`none\` on purpose. It keeps the box
     * metrics identical, so a host that sets --hermes-trigger-border to put the box back gets
     * exactly the old geometry, and nobody gets a 2px reflow in either direction. With the
     * border invisible, --hermes-trigger-hover-bg below is the only hover affordance, and the
     * :focus-visible outline above is the only focus one -- both must stay.
     */
    .trigger {
      position: relative;
      cursor: pointer;
      background: var(--hermes-trigger-bg, transparent);
      border: var(--hermes-trigger-border, 1px solid transparent);
      border-radius: var(--hermes-trigger-radius, 8px);
      /* 20px icon + 8px padding either side = a 36px target, clearing the 24x24 minimum in
         WCAG 2.2 SC 2.5.8. Do not tighten this now that the border is gone. */
      padding: var(--hermes-trigger-padding, 8px);
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

    /*
     * A restrained default for a declared level: an inset rail rather than an icon.
     *
     * An icon would need a size, a colour, an accessible name and a glyph per level -- four
     * design-system decisions this element has no business making for its host. A rail makes
     * the level visible, adds nothing to the accessibility tree (the level is decoration; the
     * text carries the meaning), and a host wanting icons can add one on
     * ::part(notification level-error)::before.
     *
     * Rows with no level are untouched, so nothing changes for existing embeds.
     */
    .notification[data-level] {
      box-shadow: inset 3px 0 0 var(--hermes-level-color, transparent);
    }
    .notification[data-level="info"] {
      --hermes-level-color: var(--hermes-level-info-color, #3b82f6);
    }
    .notification[data-level="success"] {
      --hermes-level-color: var(--hermes-level-success-color, #16a34a);
    }
    .notification[data-level="warning"] {
      --hermes-level-color: var(--hermes-level-warning-color, #d97706);
    }
    .notification[data-level="error"] {
      --hermes-level-color: var(--hermes-level-error-color, #dc2626);
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

    /* Owns the column that the row target and the expand toggle share. The toggle is a
       sibling of the target rather than a child, because the target is an <a> or a <button>
       and nesting a control inside either is invalid and unreachable by keyboard. */
    .notification-main {
      flex: 1;
      min-width: 0;
    }

    .notification-content {
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
    /* An ellipsised title above a fully expanded body reads as a bug, so the toggle lifts
       both. One control, one concept. */
    .notification-title[data-expanded] {
      white-space: normal;
      overflow: visible;
      text-overflow: clip;
    }
    .notification-body {
      color: var(--hermes-muted-color, #666);
      font-size: 13px;
      display: -webkit-box;
      -webkit-line-clamp: var(--hermes-body-line-clamp, 2);
      line-clamp: var(--hermes-body-line-clamp, 2);
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    /* Leaving -webkit-box is what actually lifts the clamp; unsetting the line count alone
       does not, in either engine. */
    .notification-body[data-expanded] {
      display: block;
      -webkit-line-clamp: none;
      line-clamp: none;
      overflow: visible;
    }

    .expand-toggle {
      background: none;
      border: none;
      padding: 2px 0;
      margin-top: 4px;
      cursor: pointer;
      font-size: 12px;
      font-weight: 500;
      color: var(--hermes-expand-color, var(--hermes-accent-color, #3b82f6));
    }
    .expand-toggle:hover {
      text-decoration: underline;
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
    this.stopObservingRows();
  }

  updated(): void {
    // Safe to call unconditionally: configure() compares against the applied config and
    // returns early when nothing changed. There is deliberately no watch list here — that
    // is what previously let `user-id` changes go unnoticed while double-applying the two
    // fields that *were* watched.
    this.applyConfig();
    this.measureRows();
  }

  /**
   * Work out which rows are clipped, and keep the observer pointed at the current ones.
   *
   * Runs after every render. It re-renders at most once more, because the toggle it may add
   * sits *below* the body and so cannot change the body's own box — the second pass measures
   * identically and changes nothing.
   */
  private measureRows(): void {
    if (!this.open) {
      this.stopObservingRows();
      return;
    }

    const rows = this.renderRoot.querySelectorAll<HTMLElement>(".notification[data-id]");
    const next = new Set<string>();
    const live = new Set<string>();

    for (const row of rows) {
      const id = row.dataset.id;
      if (id === undefined) continue;
      live.add(id);

      const title = row.querySelector<HTMLElement>(".notification-title");
      const body = row.querySelector<HTMLElement>(".notification-body");
      if (!title || !body) continue;

      this.observeRow(body);

      // An expanded row has its clamp lifted, so it measures as fitting. Re-measuring it would
      // drop it from the set, remove the button the user just pressed, and take the focus with
      // it — so its membership is carried over untouched instead.
      if (this.expanded.has(id)) {
        next.add(id);
        continue;
      }

      const truncated = this.overflowProbe
        ? this.overflowProbe({ id, title, body })
        : isTruncated(body) || isTruncated(title);
      if (truncated) next.add(id);
    }

    // Drop ids for rows that have left the list, so archiving or refreshing does not leak
    // state for the lifetime of the session.
    const expanded = intersect(this.expanded, live);
    if (expanded.size !== this.expanded.size) this.expanded = expanded;

    if (!sameMembers(next, this.overflowing)) this.overflowing = next;
  }

  private observeRow(body: HTMLElement): void {
    // jsdom has no ResizeObserver, and an unguarded constructor would throw on import of
    // every test in this package rather than in one obvious place.
    if (typeof ResizeObserver === "undefined") return;
    this.resizeObserver ??= new ResizeObserver(() => this.scheduleRemeasure());
    // observe() on an already-observed target is a no-op, so this needs no bookkeeping.
    this.resizeObserver.observe(body);
  }

  /**
   * Re-measure once on the next frame.
   *
   * Coalesced because a viewport resize fires the observer once per row, and because writing
   * to reactive state from inside a ResizeObserver callback synchronously is what produces
   * "ResizeObserver loop completed with undelivered notifications" in the console.
   */
  private scheduleRemeasure(): void {
    if (this.remeasureHandle !== undefined) return;
    this.remeasureHandle = requestAnimationFrame(() => {
      this.remeasureHandle = undefined;
      // Clearing the non-expanded entries forces the next pass to measure them afresh; without
      // it a row that stopped being clipped would keep its toggle.
      this.overflowing = intersect(this.overflowing, this.expanded);
      this.requestUpdate();
    });
  }

  private stopObservingRows(): void {
    this.resizeObserver?.disconnect();
    this.resizeObserver = undefined;
    if (this.remeasureHandle !== undefined) {
      cancelAnimationFrame(this.remeasureHandle);
      this.remeasureHandle = undefined;
    }
  }

  /** Expand or collapse one row. Deliberately does not mark it read or follow its action. */
  private toggleExpanded(id: string): void {
    const next = new Set(this.expanded);
    if (!next.delete(id)) next.add(id);
    this.expanded = next;
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

    // A level that only ever appeared in a toast, for four seconds, would be half a feature:
    // reopening the panel should still show which rows were errors. The token goes on the same
    // multi-token `part` the read/unread state already uses, so a host can select on it.
    const level = notificationLevel(notification);
    const isExpanded = this.expanded.has(notification.id);
    const canExpand = this.overflowing.has(notification.id);
    // Notification ids are Base62 (`internal/id/v2`), so they are safe in an id attribute and
    // in a selector with no escaping. Anything else here would need CSS.escape.
    const bodyId = `hermes-body-${notification.id}`;

    const body = html`
      <div class="notification-title" part="title" ?data-expanded=${isExpanded}>
        ${notification.title}
      </div>
      <div class="notification-body" part="body" id=${bodyId} ?data-expanded=${isExpanded}>
        ${notification.body}
      </div>
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
    //
    // Both carry part="row-target" so a host can reach the row itself; previously only the
    // <a> was addressable, and the plain <button> row could not be styled from outside at all.
    const target = hasAction
      ? html`<a
          class="row-target"
          part="row-target action-link"
          href=${actionUrl ?? "#"}
          @click=${(event: Event) => this.onActionActivate(event, notification)}
        >
          <div class="notification-content" part="notification-content">${body}</div>
        </a>`
      : html`<button
          class="row-target"
          part="row-target"
          type="button"
          @click=${() => this.onRowActivate(notification)}
        >
          <div class="notification-content" part="notification-content">${body}</div>
        </button>`;

    return html`
      <div
        class="notification ${isUnread ? "unread" : ""}"
        part="notification ${isUnread ? "unread" : "read"}${isExpanded ? " expanded" : ""}${
          level ? ` level-${level}` : ""
        }"
        data-id=${notification.id}
        data-level=${level ?? nothing}
      >
        ${isUnread
          ? html`<div class="unread-dot" part="unread-dot"></div>`
          : html`<div class="read-dot" part="read-dot"></div>`}
        <div class="notification-main">
          ${target}
          <!--
            A sibling of the row target, never a child: the target is an <a> or a <button>, and
            a control nested inside either is invalid and unreachable by keyboard.

            Because it sits outside the target and the row itself carries no click handler,
            expanding provably cannot mark the row read or follow its action. There is
            deliberately no stopPropagation() here — there is nothing to stop, and adding it
            would hide a regression if that ever stopped being true.
          -->
          ${canExpand
            ? html`<button
                class="expand-toggle"
                part="expand-toggle"
                type="button"
                aria-expanded=${isExpanded ? "true" : "false"}
                aria-controls=${bodyId}
                aria-label="${isExpanded ? this.collapseText : this.expandText} of ${notification.title}"
                @click=${() => this.toggleExpanded(notification.id)}
              >
                ${isExpanded ? this.collapseText : this.expandText}
              </button>`
            : nothing}
        </div>
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

/** The members of `set` that are also in `keep`. */
function intersect(set: ReadonlySet<string>, keep: ReadonlySet<string>): Set<string> {
  const out = new Set<string>();
  for (const value of set) if (keep.has(value)) out.add(value);
  return out;
}

/** Whether two sets hold the same members. Guards the re-render, so measuring cannot loop. */
function sameMembers(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}

declare global {
  interface HTMLElementTagNameMap {
    "hermes-inbox": HermesInbox;
  }
}
