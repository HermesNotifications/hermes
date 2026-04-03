"use server";

import { getHermes } from "@/lib/hermes";

export async function listTenants() {
  const hermes = getHermes();
  return hermes.tenants.list();
}
