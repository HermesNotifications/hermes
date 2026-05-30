// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use server";

import { getHermes } from "@/lib/hermes";

export async function listUsers(tenantId?: string) {
  const hermes = getHermes();
  return hermes.users.list(tenantId);
}
