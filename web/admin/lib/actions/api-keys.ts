// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use server";

import { revalidatePath } from "next/cache";
import { getHermes } from "@/lib/hermes";

export async function listAPIKeys() {
  const hermes = getHermes();
  return hermes.apiKeys.list();
}

export async function createAPIKey(data: {
  name: string;
  /** The organization the key may act for. Required — see ADR 0011. */
  organization_id: string;
  permissions?: string[];
}) {
  const hermes = getHermes();
  const result = await hermes.apiKeys.create(data);
  revalidatePath("/api-keys");
  return result;
}

export async function deleteAPIKey(id: string) {
  const hermes = getHermes();
  await hermes.apiKeys.delete(id);
  revalidatePath("/api-keys");
}
