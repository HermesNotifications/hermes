// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
