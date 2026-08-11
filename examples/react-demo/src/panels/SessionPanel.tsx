// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useState } from "react";
import type { RealtimeStatus } from "@hermes-notifications/client";
import type { LogEntry } from "../App.js";
import type { DemoSession } from "../session.js";

interface SessionPanelProps {
  session: DemoSession | null;
  realtime: RealtimeStatus;
  unreadCount: number;
  log: LogEntry[];
  error: string | null;
  onRefresh: () => void;
  onBecomeUser: (externalUserId: string) => void;
}

/**
 * Shows exactly what the browser knows, which is the security story made visible.
 *
 * Note what appears here and what does not: a short-lived user token and the internal user id, but
 * no API key — that never leaves the demo server. The `sub` row is the one worth pointing at during
 * a walkthrough, because it is the value the Centrifugo channel is named after and the one an
 * integrator would otherwise have to hand-decode from a JWT.
 */
export function SessionPanel({
  session,
  realtime,
  unreadCount,
  log,
  error,
  onRefresh,
  onBecomeUser,
}: SessionPanelProps) {
  const [userId, setUserId] = useState("demo-user-2");

  return (
    <section className="card">
      <h2>Session</h2>
      <p className="hint">What this browser holds, and nothing it should not.</p>

      <dl className="session">
        <dt>Organization</dt>
        <dd data-testid="session-org">{session?.organizationId ?? "—"}</dd>

        <dt>Your user id</dt>
        <dd data-testid="session-external-id">{session?.externalUserId ?? "—"}</dd>

        <dt>Hermes sub</dt>
        <dd data-testid="session-hermes-id">{session?.hermesUserId ?? "—"}</dd>

        <dt>Channel</dt>
        <dd>{session ? `user#${session.hermesUserId}` : "—"}</dd>

        <dt>Token expires</dt>
        <dd>{session ? new Date(session.expiresAt).toLocaleTimeString() : "—"}</dd>

        <dt>Realtime</dt>
        <dd>
          <span className="pill" data-status={realtime} data-testid="realtime-status">
            {realtime}
          </span>
        </dd>

        <dt>Unread</dt>
        <dd data-testid="session-unread">{unreadCount}</dd>
      </dl>

      <div className="button-row" style={{ marginTop: 14 }}>
        <button type="button" onClick={onRefresh} data-testid="rotate-token">
          Rotate token
        </button>
      </div>

      <div style={{ marginTop: 14 }}>
        <label htmlFor="become-user">Act as another user</label>
        <div className="field-row">
          <input
            id="become-user"
            type="text"
            value={userId}
            onChange={(event) => setUserId(event.target.value)}
          />
          <button type="button" onClick={() => onBecomeUser(userId)} data-testid="become-user">
            Switch
          </button>
        </div>
        <p className="hint" style={{ margin: 0 }}>
          Open a second window as a different user to watch channel isolation: a send to one never
          reaches the other.
        </p>
      </div>

      {error ? <p className="error">{error}</p> : null}

      <ul className="log" data-testid="activity-log">
        {log.map((entry, index) => (
          <li key={`${entry.at}-${index}`}>
            {entry.at} {entry.message}
          </li>
        ))}
      </ul>
    </section>
  );
}
