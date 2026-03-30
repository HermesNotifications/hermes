"use server";

import { revalidatePath } from "next/cache";
import { getHermes } from "@/lib/hermes";

export async function listCategories() {
  const hermes = getHermes();
  return hermes.categories.list();
}

export async function createCategory(data: {
  slug: string;
  name: string;
  defaultChannels?: string[];
  defaultState?: "on" | "off" | "required";
  sortOrder?: number;
}) {
  const hermes = getHermes();
  const result = await hermes.categories.create(data);
  revalidatePath("/categories");
  return result;
}

export async function updateCategory(
  id: string,
  data: {
    name?: string;
    defaultChannels?: string[];
    defaultState?: "on" | "off" | "required";
    sortOrder?: number;
  }
) {
  const hermes = getHermes();
  const result = await hermes.categories.update(id, data);
  revalidatePath("/categories");
  return result;
}

export async function deleteCategory(id: string) {
  const hermes = getHermes();
  await hermes.categories.delete(id);
  revalidatePath("/categories");
}
