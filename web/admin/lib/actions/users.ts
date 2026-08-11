// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use server";

import { getHermes } from "@/lib/hermes";

export async function listUsers(organizationId?: string) {
  const hermes = getHermes();
  return hermes.users.list(organizationId);
}
