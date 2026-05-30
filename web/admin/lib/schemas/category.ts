// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { z } from "zod";

export const categorySchema = z.object({
  name: z.string().min(1, "Name is required"),
  slug: z
    .string()
    .min(1, "Slug is required")
    .regex(/^[a-z0-9-]+$/, "Slug must be lowercase alphanumeric with hyphens"),
  defaultChannels: z.array(z.enum(["email", "sms", "inbox"])).optional(),
  defaultState: z.enum(["on", "off", "required"]),
  sortOrder: z.number().int().min(0),
});

export type CategoryFormData = z.infer<typeof categorySchema>;

export const subscriptionSchema = z.object({
  name: z.string().min(1, "Name is required"),
  slug: z
    .string()
    .min(1, "Slug is required")
    .regex(/^[a-z0-9-]+$/, "Slug must be lowercase alphanumeric with hyphens"),
  sortOrder: z.number().int().min(0),
});

export type SubscriptionFormData = z.infer<typeof subscriptionSchema>;
