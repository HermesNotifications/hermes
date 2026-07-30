// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { HermesProvider } from "@hermes-notifications/react";
import type { NewNotificationEvent, RealtimeStatus } from "@hermes-notifications/client";
import { Header } from "./host/Header.js";
import { Sidebar } from "./host/Sidebar.js";
import { Content } from "./host/Content.js";
import { SendPanel } from "./panels/SendPanel.js";
import { SessionPanel } from "./panels/SessionPanel.js";
import { fetchSession, login, refreshDelayMs, type DemoSession } from "./session.js";

/** Stable per-browser identity, so a reload stays the same user and the inbox persists. */
const DEFAULT_ORGANIZATION = "3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11";
const DEFAULT_USER = "demo-user-1";

export type Theme = "default" | "dark" | "brand";

/** A short activity log, so a human watching can see the pipeline working. */
export interface LogEntry {
  at: string;
  message: string;
}

export function App() {
  const [session, setSession] = useState<DemoSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [theme, setTheme] = useState<Theme>("default");
  const [realtime, setRealtime] = useState<RealtimeStatus>("disconnected");
  const [unreadCount, setUnreadCount] = useState(0);
  const [log, setLog] = useState<LogEntry[]>([]);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const append = useCallback((message: string) => {
    setLog((entries) =>
      [{ at: new Date().toLocaleTimeString(), message }, ...entries].slice(0, 40)
    );
  }, []);

  /** Establish (or re-establish) a session, then schedule the next renewal. */
  const refreshSession = useCallback(
    async (identity?: { organizationId: string; externalUserId: string }) => {
      try {
        if (identity) await login(identity);
        const next = await fetchSession();
        setSession(next);
        setError(null);
        append(`session for ${next.externalUserId} (sub ${next.hermesUserId})`);

        if (refreshTimer.current) clearTimeout(refreshTimer.current);
        refreshTimer.current = setTimeout(() => {
          void refreshSession();
        }, refreshDelayMs(next.expiresAt));
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    },
    [append]
  );

  useEffect(() => {
    void refreshSession({
      organizationId: DEFAULT_ORGANIZATION,
      externalUserId: DEFAULT_USER,
    });
    return () => {
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
    };
  }, [refreshSession]);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  // A handle for the browser suite. Token refresh cannot be tested by waiting, because the admin
  // API refuses an `expires_in` below 3600 seconds — so the test forces one instead of sleeping.
  useEffect(() => {
    (window as unknown as Record<string, unknown>).__hermesDemo = {
      refreshSession: () => refreshSession(),
      becomeUser: (externalUserId: string) =>
        refreshSession({ organizationId: DEFAULT_ORGANIZATION, externalUserId }),
      getSession: () => session,
    };
  }, [refreshSession, session]);

  const onNotification = useCallback(
    (event: NewNotificationEvent) => append(`arrived over websocket: ${event.title}`),
    [append]
  );

  /**
   * One client for the whole app, so the widget in the header and the standalone badge in the page
   * body share a single Centrifugo connection — and therefore cannot disagree. Without a provider
   * each would open its own socket for the same user.
   *
   * `getToken` is what keeps a long-lived session alive: the client calls it when the socket
   * reconnects, and once before retrying a REST call that returned 401.
   */
  const config = useMemo(
    () =>
      session
        ? {
            apiUrl: window.location.origin,
            socketUrl: session.socketUrl,
            token: session.token,
            getToken: async () => (await fetchSession()).token,
          }
        : undefined,
    [session]
  );

  const shell = (
    <div className="shell">
      <Sidebar />
      <div>
        <Header
          session={session}
          theme={theme}
          onThemeChange={setTheme}
          onNotification={onNotification}
          onUnreadCountChange={setUnreadCount}
          onRealtimeChange={setRealtime}
          onError={(message) => append(`widget: ${message}`)}
        />
        <main>
          <Content unreadCount={unreadCount} />
          <div className="side-column">
            <SendPanel disabled={!session} onSent={(what) => append(`sent: ${what}`)} />
            <SessionPanel
              session={session}
              realtime={realtime}
              unreadCount={unreadCount}
              log={log}
              error={error}
              onRefresh={() => void refreshSession()}
              onBecomeUser={(externalUserId) =>
                void refreshSession({
                  organizationId: DEFAULT_ORGANIZATION,
                  externalUserId,
                })
              }
            />
          </div>
        </main>
      </div>
    </div>
  );

  return config ? <HermesProvider config={config}>{shell}</HermesProvider> : shell;
}
