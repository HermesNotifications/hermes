"use server";

import { revalidatePath } from "next/cache";
import { getHermes } from "@/lib/hermes";

export async function listTemplates() {
  const hermes = getHermes();
  return hermes.templates.list();
}

export async function createTemplate(data: {
  slug: string;
  name: string;
  subscriptionId?: string;
  defaultChannels?: string[];
  emailSubject?: string;
  emailBody?: string;
  smsBody?: string;
  inboxTitle?: string;
  inboxBody?: string;
}) {
  const hermes = getHermes();
  const result = await hermes.templates.create(data);
  revalidatePath("/templates");
  return result;
}

export async function updateTemplate(
  id: string,
  data: {
    name?: string;
    defaultChannels?: string[];
    emailSubject?: string;
    emailBody?: string;
    smsBody?: string;
    inboxTitle?: string;
    inboxBody?: string;
  }
) {
  const hermes = getHermes();
  const result = await hermes.templates.update(id, data);
  revalidatePath("/templates");
  return result;
}

export async function deleteTemplate(id: string) {
  const hermes = getHermes();
  await hermes.templates.delete(id);
  revalidatePath("/templates");
}
