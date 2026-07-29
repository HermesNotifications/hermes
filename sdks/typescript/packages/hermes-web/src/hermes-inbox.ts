// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { LitElement, html, css, nothing, type TemplateResult } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import {
  HermesClient,
  type Notification,
  type NewNotificationEvent,
  type InboxUpdatedEvent,
} from "@hermes-notifications/client";

@customElement("hermes-inbox")
export class HermesInbox extends LitElement {
  @property({ attribute: "api-url" }) apiUrl = "";
  @property({ attribute: "socket-url" }) socketUrl = "";
  @property() token = "";
  @property({ attribute: "user-id" }) userId = "";

  @state() private open = false;
  @state() private notifications: Notification[] = [];
  @state() private unreadCount = 0;
  @state() private loading = false;
  @state() private cursor?: string;

  private client?: HermesClient;
  private cleanups: Array<() => void> = [];

  static styles = css`
    :host {
      display: inline-block;
      position: relative;
      font-family: var(--hermes-font-family, system-ui, -apple-system, sans-serif);
      font-size: var(--hermes-font-size, 14px);
      color: var(--hermes-text-color, #1a1a1a);
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
      min-width: 18px;
      height: 18px;
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
      box-shadow: var(--hermes-popover-shadow, 0 8px 30px rgba(0,0,0,0.12));
      display: flex;
      flex-direction: column;
      overflow: hidden;
      z-index: 1000;
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
    }

    .notification {
      display: flex;
      gap: 12px;
      padding: 12px 16px;
      border-bottom: 1px solid var(--hermes-border-color, #e0e0e0);
      cursor: pointer;
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

    .actions {
      display: flex;
      gap: 4px;
      flex-shrink: 0;
      opacity: 0;
      transition: opacity 0.1s;
    }
    .notification:hover .actions {
      opacity: 1;
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

    .empty {
      padding: 40px 16px;
      text-align: center;
      color: var(--hermes-muted-color, #666);
    }

    .loading {
      padding: 20px;
      text-align: center;
      color: var(--hermes-muted-color, #666);
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.initClient();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.cleanups.forEach((fn) => fn());
    this.cleanups = [];
    this.client?.disconnect();
    this.client = undefined;
  }

  updated(changed: Map<string, unknown>) {
    if (changed.has("token") || changed.has("apiUrl")) {
      this.initClient();
    }
  }

  private async initClient() {
    if (!this.apiUrl || !this.token) return;

    this.cleanups.forEach((fn) => fn());
    this.cleanups = [];
    this.client?.disconnect();

    this.client = new HermesClient({
      apiUrl: this.apiUrl,
      socketUrl: this.socketUrl || undefined,
      token: this.token,
    });

    this.cleanups.push(
      this.client.on("notification", (e: NewNotificationEvent) => {
        const notif: Notification = {
          id: e.id,
          title: e.title,
          body: e.body,
          status: "delivered",
          channels: ["inbox"],
          created_at: e.createdAt,
          organization_id: "",
          user_id: "",
          category_id: "",
        };
        this.notifications = [notif, ...this.notifications];
        this.unreadCount++;
        this.dispatchEvent(new CustomEvent("notification", { detail: e }));
      })
    );

    this.cleanups.push(
      this.client.on("update", (e: InboxUpdatedEvent) => {
        this.unreadCount = e.unreadCount;
        if (e.action === "read" || e.action === "archive" || e.action === "delete") {
          this.notifications = this.notifications.map((n) =>
            n.id === e.notificationId
              ? { ...n, read_at: e.action === "read" ? new Date().toISOString() : n.read_at }
              : n
          );
          if (e.action === "delete") {
            this.notifications = this.notifications.filter((n) => n.id !== e.notificationId);
          }
        }
        if (e.action === "read-all") {
          this.notifications = this.notifications.map((n) => ({
            ...n,
            read_at: n.read_at ?? new Date().toISOString(),
          }));
        }
        this.dispatchEvent(new CustomEvent("update", { detail: e }));
      })
    );

    this.cleanups.push(
      this.client.on("unreadCountChange", (count: number) => {
        this.dispatchEvent(new CustomEvent("unread-count-change", { detail: count }));
      })
    );

    await this.loadNotifications();

    if (this.userId) {
      this.client.connect(this.userId).catch((err) => {
        console.error("Hermes: failed to connect real-time:", err);
      });
    }
  }

  private async loadNotifications() {
    if (!this.client) return;
    this.loading = true;
    try {
      const page = await this.client.inbox.list({ limit: 20 });
      this.notifications = page.data;
      this.unreadCount = page.unreadCount;
      this.cursor = page.cursor;
    } catch (err) {
      console.error("Hermes: failed to load notifications:", err);
    } finally {
      this.loading = false;
    }
  }

  private toggle() {
    this.open = !this.open;
  }

  private async handleMarkRead(e: Event, id: string) {
    e.stopPropagation();
    await this.client?.inbox.markRead(id);
    this.notifications = this.notifications.map((n) =>
      n.id === id ? { ...n, read_at: new Date().toISOString() } : n
    );
    this.unreadCount = Math.max(0, this.unreadCount - 1);
  }

  private async handleArchive(e: Event, id: string) {
    e.stopPropagation();
    await this.client?.inbox.archive(id);
    this.notifications = this.notifications.filter((n) => n.id !== id);
  }

  private async handleMarkAllRead() {
    await this.client?.inbox.markAllRead();
    this.notifications = this.notifications.map((n) => ({
      ...n,
      read_at: n.read_at ?? new Date().toISOString(),
    }));
    this.unreadCount = 0;
  }

  private formatTime(dateStr: string): string {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const diffDay = Math.floor(diffHr / 24);
    if (diffDay < 7) return `${diffDay}d ago`;
    return date.toLocaleDateString();
  }

  private renderBellIcon(): TemplateResult {
    return html`<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>`;
  }

  render() {
    return html`
      <button class="trigger" part="trigger" @click=${this.toggle} aria-label="Notifications">
        ${this.renderBellIcon()}
        ${this.unreadCount > 0
          ? html`<span class="badge" part="badge">${this.unreadCount > 99 ? "99+" : this.unreadCount}</span>`
          : nothing}
      </button>

      ${this.open
        ? html`
            <div class="popover" part="popover">
              <div class="header" part="header">
                <span>Notifications</span>
                ${this.unreadCount > 0
                  ? html`<button class="mark-all-read" @click=${this.handleMarkAllRead}>Mark all read</button>`
                  : nothing}
              </div>
              <div class="list" part="list">
                ${this.loading
                  ? html`<div class="loading">Loading...</div>`
                  : this.notifications.length === 0
                    ? html`<div class="empty" part="empty">No notifications</div>`
                    : this.notifications.map((n) => this.renderNotification(n))}
              </div>
            </div>
          `
        : nothing}
    `;
  }

  private renderNotification(n: Notification): TemplateResult {
    const isUnread = !n.read_at;
    return html`
      <div class="notification ${isUnread ? "unread" : ""}" part="notification">
        ${isUnread
          ? html`<div class="unread-dot" part="unread-dot"></div>`
          : html`<div class="read-dot"></div>`}
        <div class="notification-content">
          <div class="notification-title" part="title">${n.title}</div>
          <div class="notification-body" part="body">${n.body}</div>
          <div class="notification-time" part="time">${this.formatTime(n.created_at)}</div>
        </div>
        <div class="actions">
          ${isUnread
            ? html`<button class="action-btn" @click=${(e: Event) => this.handleMarkRead(e, n.id)}>Read</button>`
            : nothing}
          <button class="action-btn" @click=${(e: Event) => this.handleArchive(e, n.id)}>Archive</button>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "hermes-inbox": HermesInbox;
  }
}
