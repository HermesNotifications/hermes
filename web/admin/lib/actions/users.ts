"use server";

import { getHermes } from "@/lib/hermes";

export async function listUsers(tenantId?: string) {
  const hermes = getHermes();
  return hermes.users.list(tenantId);
}
