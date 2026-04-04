"use server";

import { getHermes } from "@/lib/hermes";

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
