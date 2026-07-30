// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { createContext, useContext, type ReactNode } from "react";
import type { HermesClient, HermesClientConfig } from "@hermes-notifications/client";
import { useHermesClient } from "./hooks.js";

const HermesContext = createContext<HermesClient | null>(null);

export interface HermesProviderProps {
  /** A client you already own. Takes precedence over `config`. */
  client?: HermesClient;
  /** Config to build a client from, owned by the provider for its lifetime. */
  config?: HermesClientConfig;
  children: ReactNode;
}

/**
 * Configure Hermes once for a subtree.
 *
 * Worth using even for a single widget: everything below shares one client and therefore one
 * Centrifugo connection. Two components each calling `useHermesClient` would open two sockets
 * for the same user.
 */
export function HermesProvider({ client, config, children }: HermesProviderProps) {
  const built = useHermesClientOrNull(config);
  return (
    <HermesContext.Provider value={client ?? built}>{children}</HermesContext.Provider>
  );
}

/**
 * Build a client from `config`, or return null.
 *
 * A separate component would be the tidier way to make this conditional, but hooks cannot be
 * called conditionally — so the config is normalised to a placeholder and the result discarded
 * when there was nothing real to build. The placeholder client opens no socket (nothing calls
 * `connect`), so this costs nothing.
 */
function useHermesClientOrNull(config?: HermesClientConfig): HermesClient | null {
  const client = useHermesClient(config ?? { apiUrl: "", token: "" });
  return config ? client : null;
}

/**
 * The client supplied by the nearest {@link HermesProvider}, or null.
 *
 * Returns null rather than throwing when there is no provider, so a component can be mounted
 * both inside and outside one — the usual shape while an app is adopting Hermes incrementally.
 */
export function useHermes(): HermesClient | null {
  return useContext(HermesContext);
}
