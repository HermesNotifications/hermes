// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { CategoryForm } from "@/components/category-form";
import { createCategory } from "@/lib/actions/categories";
import type { CategoryFormData } from "@/lib/schemas/category";

export default function NewCategoryPage() {
  async function handleCreate(data: CategoryFormData) {
    "use server";
    await createCategory({
      slug: data.slug,
      name: data.name,
      defaultChannels: data.defaultChannels,
      defaultState: data.defaultState,
      sortOrder: data.sortOrder,
    });
  }

  return (
    <div className="space-y-6">
      <div>
        <Link
          href="/categories"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ChevronLeftIcon className="size-4" />
          Categories
        </Link>
        <h1 className="text-2xl font-semibold">New Category</h1>
        <p className="text-sm text-muted-foreground">
          Create a new subscription category.
        </p>
      </div>

      <CategoryForm onSubmit={handleCreate} />
    </div>
  );
}
