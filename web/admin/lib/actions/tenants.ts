"use server";

import { getHermes } from "@/lib/hermes";
import { revalidatePath } from "next/cache";

export async function listTenants() {
  const hermes = getHermes();
  return hermes.tenants.list();
}

export async function createTenant(name: string) {
  const hermes = getHermes();
  const result = await hermes.tenants.create({ name });
  revalidatePath("/tenants");
  return result;
}
