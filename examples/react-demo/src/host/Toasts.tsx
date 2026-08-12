// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { Toaster } from "sonner";
import { useHermes, useHermesToasts } from "@hermes-notifications/react";
import { sonnerAdapter } from "@hermes-notifications/react/sonner";

/**
 * Toasts for arriving notifications that ask for one.
 *
 * ## What this demonstrates
 *
 * The SDK renders no toast UI of its own and has no runtime dependency on sonner. It gives you
 * a hook and an adapter interface; `sonnerAdapter` is one implementation, shipped on a separate
 * subpath so that sonner stays an *optional* peer dependency. Swapping to react-hot-toast, or
 * to your design system's own snackbar, means passing a different object with the same five
 * methods — there is nothing else to change.
 *
 * ## Two behaviours worth knowing before you assume they are bugs
 *
 * **Only live arrivals toast.** The hook listens to websocket publications, not to the initial
 * list and not to the REST repair that runs after a reconnect. Loading the page with forty
 * unread notifications produces no toasts, and neither does a laptop waking up.
 *
 * **Toasts fire whether or not the panel is open.** The SDK cannot know — a headless host has
 * no panel. If you want to suppress them while the user is already looking at their inbox,
 * track `onOpenChange` on `<HermesInbox>` and read it from `shouldToast`.
 */
export function Toasts() {
  const client = useHermes();

  useHermesToasts(client, {
    toast: sonnerAdapter,
    // Retract a toast if the notification is read elsewhere -- another tab, another device.
    dismissOnRead: true,
  });

  return (
    <Toaster
      position="top-right"
      richColors
      closeButton
      // sonner's default container label is "Notifications", which would put a second region by
      // that name in the accessibility tree next to the bell, whose label is also
      // "Notifications". Naming it explicitly keeps the two distinguishable to a screen reader.
      containerAriaLabel="Toasts"
    />
  );
}
