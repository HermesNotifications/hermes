// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { notFound } from "next/navigation";
import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { CategoryForm } from "@/components/category-form";
import { SubscriptionList } from "@/components/subscription-list";
import { Separator } from "@/components/ui/separator";
import { listCategories, updateCategory } from "@/lib/actions/categories";
import { listSubscriptions } from "@/lib/actions/subscriptions";
import type { CategoryFormData } from "@/lib/schemas/category";

interface CategoryDetailPageProps {
  params: Promise<{ id: string }>;
}

export default async function CategoryDetailPage({ params }: CategoryDetailPageProps) {
  const { id } = await params;

  const [categories, subscriptions] = await Promise.all([
    listCategories(),
    listSubscriptions(id),
  ]);

  const category = (categories ?? []).find((c) => c.id === id);

  if (!category) {
    notFound();
  }

  async function handleUpdate(data: CategoryFormData) {
    "use server";
    await updateCategory(id, {
      name: data.name,
      defaultChannels: data.defaultChannels,
      defaultState: data.defaultState,
      sortOrder: data.sortOrder,
    });
  }

  const defaultValues: Partial<CategoryFormData> = {
    name: category.name,
    slug: category.slug,
    defaultChannels: ((category.default_channels ?? []) as string[]).filter(
      (c: string) => ["email", "sms", "inbox"].includes(c)
    ) as Array<"email" | "sms" | "inbox">,
    defaultState: (["on", "off", "required"].includes(category.default_state)
      ? category.default_state
      : "on") as "on" | "off" | "required",
    sortOrder: category.sort_order,
  };

  return (
    <div className="space-y-8">
      <div>
        <Link
          href="/categories"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ChevronLeftIcon className="size-4" />
          Categories
        </Link>
        <h1 className="text-2xl font-semibold">Edit Category</h1>
        <p className="text-sm text-muted-foreground">
          Update <span className="font-mono">{category.slug}</span>
        </p>
      </div>

      <CategoryForm defaultValues={defaultValues} onSubmit={handleUpdate} isEdit />

      <Separator />

      <SubscriptionList categoryId={id} subscriptions={subscriptions ?? []} />
    </div>
  );
}
