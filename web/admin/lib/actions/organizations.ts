// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use server";

import { getHermes } from "@/lib/hermes";
import { revalidatePath } from "next/cache";

export async function listOrganizations() {
  const hermes = getHermes();
  return hermes.organizations.list();
}

export async function createOrganization(name: string) {
  const hermes = getHermes();
  const result = await hermes.organizations.create({ name });
  revalidatePath("/organizations");
  return result;
}
