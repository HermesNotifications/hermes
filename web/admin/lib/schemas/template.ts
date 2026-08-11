// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { z } from "zod";

export const templateSchema = z.object({
  name: z.string().min(1, "Name is required"),
  slug: z
    .string()
    .min(1, "Slug is required")
    .regex(/^[a-z0-9-]+$/, "Slug must be lowercase alphanumeric with hyphens"),
  subscriptionId: z.string().optional(),
  defaultChannels: z.array(z.enum(["email", "sms", "inbox"])).optional(),
  emailSubject: z.string().optional(),
  emailBody: z.string().optional(),
  smsBody: z.string().optional(),
  inboxTitle: z.string().optional(),
  inboxBody: z.string().optional(),
});

export type TemplateFormData = z.infer<typeof templateSchema>;
