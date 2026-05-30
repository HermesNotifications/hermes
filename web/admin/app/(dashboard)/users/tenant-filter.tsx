// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use client";

import { useRouter } from "next/navigation";
import type { Tenant } from "@hermes-notifications/server";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface TenantFilterProps {
  tenants: Tenant[];
  currentTenantId?: string;
}

export function TenantFilter({ tenants, currentTenantId }: TenantFilterProps) {
  const router = useRouter();

  function handleChange(value: string | null) {
    if (!value || value === "all") {
      router.push("/users");
    } else {
      router.push(`/users?tenant_id=${value}`);
    }
  }

  return (
    <Select value={currentTenantId ?? "all"} onValueChange={handleChange}>
      <SelectTrigger className="w-[220px]">
        <SelectValue placeholder="All tenants" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">All tenants</SelectItem>
        {tenants.map((tenant) => (
          <SelectItem key={tenant.id} value={tenant.id}>
            {tenant.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
