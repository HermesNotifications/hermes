// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import Link from "next/link";
import { PlusIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/data-table";
import { listCategories } from "@/lib/actions/categories";
import { columns } from "./columns";

export default async function CategoriesPage() {
  const categories = await listCategories();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Categories</h1>
          <p className="text-sm text-muted-foreground">
            Manage subscription categories and their notification preferences.
          </p>
        </div>
        <Button render={<Link href="/categories/new" />}>
          <PlusIcon />
          New Category
        </Button>
      </div>

      <DataTable
        columns={columns}
        data={categories ?? []}
        searchKey="name"
        searchPlaceholder="Search categories..."
      />
    </div>
  );
}
