// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { z } from "zod";

const channels = z.array(z.enum(["email", "sms", "inbox"]));

const templateModeSchema = z.object({
  mode: z.literal("template"),
  template: z.string().min(1, "Template is required"),
  data: z
    .array(z.object({ key: z.string().min(1, "Key is required"), value: z.string() }))
    .optional(),
  channels: channels.optional(),
});

const contentModeSchema = z.object({
  mode: z.literal("content"),
  title: z.string().min(1, "Title is required"),
  body: z.string().min(1, "Body is required"),
  actionUrl: z.string().optional(),
  actionLabel: z.string().optional(),
  channels: channels.min(1, "At least one channel is required for direct content"),
});

export const sendNotificationSchema = z
  .object({
    organizationId: z.string().optional(),
    newOrganizationName: z.string().optional(),
    userId: z.string().min(1, "User ID is required"),
    email: z.string().email("Invalid email").optional().or(z.literal("")),
    phone: z.string().optional(),
  })
  .and(z.discriminatedUnion("mode", [templateModeSchema, contentModeSchema]))
  .refine((data) => data.organizationId || data.newOrganizationName, {
    message: "Select an organization or create a new one",
    path: ["organizationId"],
  });

export type SendNotificationFormData = z.infer<typeof sendNotificationSchema>;
