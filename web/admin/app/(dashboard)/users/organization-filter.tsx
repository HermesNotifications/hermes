// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use client";

import { useRouter } from "next/navigation";
import type { Organization } from "@hermes-notifications/server";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface OrganizationFilterProps {
  organizations: Organization[];
  currentOrganizationId?: string;
}

export function OrganizationFilter({ organizations, currentOrganizationId }: OrganizationFilterProps) {
  const router = useRouter();

  function handleChange(value: string | null) {
    if (!value || value === "all") {
      router.push("/users");
    } else {
      router.push(`/users?organization_id=${value}`);
    }
  }

  return (
    <Select value={currentOrganizationId ?? "all"} onValueChange={handleChange}>
      <SelectTrigger className="w-[220px]">
        <SelectValue placeholder="All organizations" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">All organizations</SelectItem>
        {organizations.map((organization) => (
          <SelectItem key={organization.id} value={organization.id}>
            {organization.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
