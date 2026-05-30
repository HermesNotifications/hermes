// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "templates", Short: "Manage notification templates"}
	cmd.AddCommand(newTemplatesListCmd())
	cmd.AddCommand(newTemplatesCreateCmd())
	cmd.AddCommand(newTemplatesUpdateCmd())
	cmd.AddCommand(newTemplatesDeleteCmd())
	return cmd
}

func templateChannels(t client.NotificationTemplate) string {
	var channels []string
	if t.EmailSubject != nil {
		channels = append(channels, "email")
	}
	if t.SMSBody != nil {
		channels = append(channels, "sms")
	}
	if t.InboxTitle != nil {
		channels = append(channels, "inbox")
	}
	return strings.Join(channels, ",")
}

func newTemplatesListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all notification templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			templates, err := c.Templates.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, templates)
			}
			var rows [][]string
			for _, t := range templates {
				subID := ""
				if t.SubscriptionID != nil {
					subID = *t.SubscriptionID
				}
				rows = append(rows, []string{t.ID, t.Slug, bold(t.Name), subID, templateChannels(t), fmtTime(t.CreatedAt.Format(time.RFC3339))})
			}
			printTable(out, []string{"ID", "SLUG", "NAME", "SUBSCRIPTION", "CHANNELS", "CREATED"}, rows)
			return nil
		},
	}
}

func newTemplatesCreateCmd() *cobra.Command {
	var slug, name string
	var subscriptionID string
	var emailSubject, emailBody, smsBody, inboxTitle, inboxBody string

	cmd := &cobra.Command{
		Use: "create", Short: "Create a notification template",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.CreateTemplateRequest{
				Slug: slug,
				Name: name,
			}
			if cmd.Flags().Changed("subscription-id") {
				req.SubscriptionID = &subscriptionID
			}
			setOptionalString(cmd, "email-subject", &req.EmailSubject, emailSubject)
			setOptionalString(cmd, "email-body", &req.EmailBody, emailBody)
			setOptionalString(cmd, "sms-body", &req.SMSBody, smsBody)
			setOptionalString(cmd, "inbox-title", &req.InboxTitle, inboxTitle)
			setOptionalString(cmd, "inbox-body", &req.InboxBody, inboxBody)

			c := newClientFromCmd(cmd)
			t, err := c.Templates.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "%s %s %s\n", success("Created template"), bold(t.ID), dim("("+t.Slug+")"))
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "Template slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Template name (required)")
	cmd.Flags().StringVar(&subscriptionID, "subscription-id", "", "Subscription ID (optional)")
	cmd.Flags().StringVar(&emailSubject, "email-subject", "", "Email subject template")
	cmd.Flags().StringVar(&emailBody, "email-body", "", "Email body template")
	cmd.Flags().StringVar(&smsBody, "sms-body", "", "SMS body template")
	cmd.Flags().StringVar(&inboxTitle, "inbox-title", "", "Inbox title template")
	cmd.Flags().StringVar(&inboxBody, "inbox-body", "", "Inbox body template")
	cmd.MarkFlagRequired("slug")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newTemplatesUpdateCmd() *cobra.Command {
	var id, name string
	var emailSubject, emailBody, smsBody, inboxTitle, inboxBody string

	cmd := &cobra.Command{
		Use: "update", Short: "Update a notification template",
		RunE: func(cmd *cobra.Command, args []string) error {
			var req client.UpdateTemplateRequest
			if cmd.Flags().Changed("name") {
				req.Name = name
			}
			setOptionalString(cmd, "email-subject", &req.EmailSubject, emailSubject)
			setOptionalString(cmd, "email-body", &req.EmailBody, emailBody)
			setOptionalString(cmd, "sms-body", &req.SMSBody, smsBody)
			setOptionalString(cmd, "inbox-title", &req.InboxTitle, inboxTitle)
			setOptionalString(cmd, "inbox-body", &req.InboxBody, inboxBody)

			c := newClientFromCmd(cmd)
			t, err := c.Templates.Update(cmd.Context(), id, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "%s %s\n", success("Updated template"), bold(t.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Template ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Template name")
	cmd.Flags().StringVar(&emailSubject, "email-subject", "", "Email subject template")
	cmd.Flags().StringVar(&emailBody, "email-body", "", "Email body template")
	cmd.Flags().StringVar(&smsBody, "sms-body", "", "SMS body template")
	cmd.Flags().StringVar(&inboxTitle, "inbox-title", "", "Inbox title template")
	cmd.Flags().StringVar(&inboxBody, "inbox-body", "", "Inbox body template")
	cmd.MarkFlagRequired("id")
	return cmd
}

func newTemplatesDeleteCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "delete", Short: "Delete a notification template",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			if err := c.Templates.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, map[string]string{"status": "deleted", "id": id})
			}
			fmt.Fprintf(out, "%s %s\n", success("Deleted template"), bold(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Template ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}

// setOptionalString sets a *string pointer only when the flag was explicitly provided.
func setOptionalString(cmd *cobra.Command, flagName string, target **string, val string) {
	if cmd.Flags().Changed(flagName) {
		*target = &val
	}
}
