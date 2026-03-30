"use server";

import { getHermes } from "@/lib/hermes";

export async function getNotificationStatus(id: string) {
  const hermes = getHermes();
  try {
    return await hermes.notifications.getStatus(id);
  } catch {
    return null;
  }
}
