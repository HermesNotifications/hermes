// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use server";

import { getHermes } from "@/lib/hermes";
import { revalidatePath } from "next/cache";

export async function listRecentNotifications() {
  const hermes = getHermes();
  return hermes.notifications.list();
}

export async function getNotificationStatus(id: string) {
  const hermes = getHermes();
  try {
    return await hermes.notifications.getStatus(id);
  } catch {
    return null;
  }
}

export async function sendNotification(options: {
  to: {
    organizationId: string;
    userId: string;
    contacts?: Record<string, string>;
  };
  template?: string;
  content?: {
    title: string;
    body: string;
    actionUrl?: string;
    actionLabel?: string;
  };
  data?: Record<string, unknown>;
  channels?: string[];
}) {
  const hermes = getHermes();
  const result = await hermes.notifications.send(options);
  revalidatePath("/notifications");
  return result;
}
