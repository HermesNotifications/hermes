// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
