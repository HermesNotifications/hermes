// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use server";

import { revalidatePath } from "next/cache";
import { getHermes } from "@/lib/hermes";

export async function listTemplates() {
  const hermes = getHermes();
  return hermes.templates.list();
}

interface FlatTemplateContent {
  emailSubject?: string;
  emailBody?: string;
  smsBody?: string;
  inboxTitle?: string;
  inboxBody?: string;
}

function toContent(
  f: FlatTemplateContent
): Record<string, Record<string, string>> {
  const c: Record<string, Record<string, string>> = {};
  if (f.emailSubject || f.emailBody) {
    c.email = {
      ...(f.emailSubject ? { subject: f.emailSubject } : {}),
      ...(f.emailBody ? { body: f.emailBody } : {}),
    };
  }
  if (f.smsBody) c.sms = { body: f.smsBody };
  if (f.inboxTitle || f.inboxBody) {
    c.inbox = {
      ...(f.inboxTitle ? { title: f.inboxTitle } : {}),
      ...(f.inboxBody ? { body: f.inboxBody } : {}),
    };
  }
  return c;
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
  const { emailSubject, emailBody, smsBody, inboxTitle, inboxBody, ...rest } =
    data;
  const result = await hermes.templates.create({
    ...rest,
    content: toContent({ emailSubject, emailBody, smsBody, inboxTitle, inboxBody }),
  });
  revalidatePath("/templates");
  return result;
}

export async function updateTemplate(
  id: string,
  data: {
    name?: string;
    subscriptionId?: string;
    defaultChannels?: string[];
    emailSubject?: string;
    emailBody?: string;
    smsBody?: string;
    inboxTitle?: string;
    inboxBody?: string;
  }
) {
  const hermes = getHermes();
  const { emailSubject, emailBody, smsBody, inboxTitle, inboxBody, ...rest } =
    data;
  const result = await hermes.templates.update(id, {
    ...rest,
    content: toContent({ emailSubject, emailBody, smsBody, inboxTitle, inboxBody }),
  });
  revalidatePath("/templates");
  return result;
}

export async function deleteTemplate(id: string) {
  const hermes = getHermes();
  await hermes.templates.delete(id);
  revalidatePath("/templates");
}
