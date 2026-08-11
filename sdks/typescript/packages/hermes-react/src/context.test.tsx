// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { cleanup, render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FakeHermesClient, fakePage } from "@hermes-notifications/client/testing";
import { HermesProvider, useHermes } from "./context.js";

/**
 * The provider exists so an app configures Hermes once, near its root, and every widget and
 * hook below shares one client and therefore one socket. Without it, two components each
 * calling `useHermesClient` would open two Centrifugo connections for the same user.
 */

afterEach(cleanup);

function Probe() {
  const client = useHermes();
  return <span data-testid="probe">{client ? "has-client" : "no-client"}</span>;
}

describe("HermesProvider", () => {
  it("supplies the client to descendants", () => {
    const fake = new FakeHermesClient(fakePage());

    render(
      <HermesProvider client={fake.asClient()}>
        <Probe />
      </HermesProvider>
    );

    expect(screen.getByTestId("probe").textContent).toBe("has-client");
  });

  it("shares one client between siblings rather than one socket each", () => {
    const fake = new FakeHermesClient(fakePage());
    const seen: unknown[] = [];
    function Collect() {
      seen.push(useHermes());
      return null;
    }

    render(
      <HermesProvider client={fake.asClient()}>
        <Collect />
        <Collect />
      </HermesProvider>
    );

    expect(seen).toHaveLength(2);
    expect(seen[0]).toBe(seen[1]);
  });

  it("builds a client from config when given one instead of a client", () => {
    render(
      <HermesProvider config={{ apiUrl: "http://localhost:8888", token: "tok" }}>
        <Probe />
      </HermesProvider>
    );

    expect(screen.getByTestId("probe").textContent).toBe("has-client");
  });

  it("renders children even with neither client nor config", () => {
    // An app whose token has not arrived yet still has to render.
    render(
      <HermesProvider>
        <Probe />
      </HermesProvider>
    );

    expect(screen.getByTestId("probe").textContent).toBe("no-client");
  });
});

describe("useHermes", () => {
  it("returns null outside a provider rather than throwing", () => {
    // Throwing would make the hook unusable in a component that is sometimes mounted outside
    // the provider — a common shape in an app being incrementally adopted.
    const { result } = renderHook(() => useHermes());
    expect(result.current).toBeNull();
  });
});
