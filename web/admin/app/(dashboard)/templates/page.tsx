// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import Link from "next/link";
import { PlusIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/data-table";
import { listTemplates } from "@/lib/actions/templates";
import { columns } from "./columns";

export default async function TemplatesPage() {
  const templates = await listTemplates();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Templates</h1>
          <p className="text-sm text-muted-foreground">
            Manage notification templates for email, SMS, and inbox channels.
          </p>
        </div>
        <Button render={<Link href="/templates/new" />}>
          <PlusIcon />
          New Template
        </Button>
      </div>

      <DataTable
        columns={columns}
        data={templates}
        searchKey="name"
        searchPlaceholder="Search templates..."
      />
    </div>
  );
}
