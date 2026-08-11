// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useHermes, useUnreadCount } from "@hermes-notifications/react";

interface ContentProps {
  unreadCount: number;
}

/**
 * Filler page content, plus one thing that is not filler: a second, independent unread badge.
 *
 * It reads `useUnreadCount`, not the widget, so if the two ever disagree it is visible on screen.
 * That divergence was real until recently — the client only learned the count from a server-side
 * mutation event, so a standalone badge read zero even with unread rows on display.
 */
export function Content({ unreadCount }: ContentProps) {
  const client = useHermes();
  const hookCount = useUnreadCount(client);

  return (
    <div style={{ display: "grid", gap: 20 }}>
      <section className="card">
        <h2>This week</h2>
        <p className="hint">Placeholder content. The interesting part is in the header.</p>
        <div className="stat-row">
          <div className="stat">
            <div className="stat-label">Sessions</div>
            <div className="stat-value">12,480</div>
          </div>
          <div className="stat">
            <div className="stat-label">Conversion</div>
            <div className="stat-value">3.1%</div>
          </div>
          <div className="stat">
            <div className="stat-label">Unread notifications</div>
            <div className="stat-value" data-testid="header-unread-mirror">
              {unreadCount}
            </div>
          </div>
          <div className="stat">
            <div className="stat-label">Unread, via useUnreadCount</div>
            <div className="stat-value" data-testid="hook-unread">
              {hookCount}
            </div>
          </div>
        </div>
      </section>

      <section className="card">
        <h2>Scroll target</h2>
        <p className="hint">
          Open the inbox and scroll this page. The panel is anchored to a sticky header, which is
          where an embedded popover usually breaks.
        </p>
        <div style={{ height: 720 }} />
      </section>
    </div>
  );
}
