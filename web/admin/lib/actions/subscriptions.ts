"use server";

import { revalidatePath } from "next/cache";
import { getHermes } from "@/lib/hermes";

export async function listSubscriptions(categoryId: string) {
  const hermes = getHermes();
  return hermes.subscriptions.list(categoryId);
}

export async function listAllSubscriptions() {
  const hermes = getHermes();
  const categories = await hermes.categories.list();
  const groups = await Promise.all(
    categories.map(async (cat) => ({
      category: cat,
      subscriptions: await hermes.subscriptions.list(cat.id),
    }))
  );
  return groups;
}

export async function createSubscription(
  categoryId: string,
  data: {
    slug: string;
    name: string;
    sortOrder?: number;
  }
) {
  const hermes = getHermes();
  const result = await hermes.subscriptions.create(categoryId, data);
  revalidatePath(`/categories/${categoryId}`);
  return result;
}

export async function updateSubscription(
  id: string,
  categoryId: string,
  data: {
    name?: string;
    sortOrder?: number;
  }
) {
  const hermes = getHermes();
  const result = await hermes.subscriptions.update(id, data);
  revalidatePath(`/categories/${categoryId}`);
  return result;
}

export async function deleteSubscription(id: string, categoryId: string) {
  const hermes = getHermes();
  await hermes.subscriptions.delete(id);
  revalidatePath(`/categories/${categoryId}`);
}
