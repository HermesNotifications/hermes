// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { HermesInbox, useHermes } from "@hermes-notifications/react";
import type { NewNotificationEvent, RealtimeStatus } from "@hermes-notifications/client";
import type { Theme } from "../App.js";
import type { DemoSession } from "../session.js";

interface HeaderProps {
  session: DemoSession | null;
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
  onNotification: (event: NewNotificationEvent) => void;
  onUnreadCountChange: (count: number) => void;
  onRealtimeChange: (status: RealtimeStatus) => void;
  onError: (message: string) => void;
}

/**
 * The host application's header — and the whole point of the demo.
 *
 * The widget sits next to the avatar, inside a sticky, z-indexed header. That placement is
 * deliberate: it is exactly where an embedded popover is clipped by an ancestor's stacking context
 * or overflow in a real application, and it is the arrangement no jsdom test can evaluate.
 */
export function Header({
  session,
  theme,
  onThemeChange,
  onNotification,
  onUnreadCountChange,
  onRealtimeChange,
  onError,
}: HeaderProps) {
  const client = useHermes();

  return (
    <header className="header">
      <h1 className="header-title">Dashboard</h1>

      <input
        className="header-search"
        type="text"
        placeholder="Search reports…"
        aria-label="Search reports"
      />

      <div className="header-actions">
        <select
          aria-label="Theme"
          value={theme}
          onChange={(event) => onThemeChange(event.target.value as Theme)}
        >
          <option value="default">Default</option>
          <option value="dark">Dark</option>
          <option value="brand">Brand</option>
        </select>

        {/*
          Everything the widget needs, and nothing more.

          Note what is absent: no API key, and no Centrifugo configuration — the channel is derived
          from the token's `sub` claim by the client, so there is no chance of subscribing to the
          wrong one.

          The client comes from HermesProvider so the widget and the standalone badge lower down the
          page share one socket. A consumer who does not want a provider can pass `apiUrl` +
          `tokenUrl` instead and the widget will manage its own client and its own refresh; that is
          the plain-HTML path.
        */}
        <HermesInbox
          client={client ?? undefined}
          userId={session?.hermesUserId}
          pageSize={20}
          onNotification={onNotification}
          onUnreadCountChange={onUnreadCountChange}
          onRealtimeChange={onRealtimeChange}
          onError={(error) => onError(error.message)}
          onAction={(notification, event) => {
            // A single-page app routes internally rather than losing its state to a full page
            // navigation. Cancelling the event is how the widget lets it.
            event.preventDefault();
            onError(`action followed: ${notification.action_url ?? "(none)"}`);
          }}
        />

        <div className="avatar" title={session?.externalUserId ?? "signed out"}>
          {(session?.externalUserId ?? "?").slice(0, 2).toUpperCase()}
        </div>
      </div>
    </header>
  );
}
