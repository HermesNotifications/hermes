// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package cli

import (
	"fmt"
	"sort"
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
	for slug, fields := range t.Content {
		for _, v := range fields {
			if v != "" {
				channels = append(channels, slug)
				break
			}
		}
	}
	sort.Strings(channels)
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
			content := map[string]map[string]string{}
			setContentField(cmd, content, "email-subject", "email", "subject", emailSubject)
			setContentField(cmd, content, "email-body", "email", "body", emailBody)
			setContentField(cmd, content, "sms-body", "sms", "body", smsBody)
			setContentField(cmd, content, "inbox-title", "inbox", "title", inboxTitle)
			setContentField(cmd, content, "inbox-body", "inbox", "body", inboxBody)
			if len(content) > 0 {
				req.Content = content
			}

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
			content := map[string]map[string]string{}
			setContentField(cmd, content, "email-subject", "email", "subject", emailSubject)
			setContentField(cmd, content, "email-body", "email", "body", emailBody)
			setContentField(cmd, content, "sms-body", "sms", "body", smsBody)
			setContentField(cmd, content, "inbox-title", "inbox", "title", inboxTitle)
			setContentField(cmd, content, "inbox-body", "inbox", "body", inboxBody)
			if len(content) > 0 {
				req.Content = content
			}

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

// setContentField populates content[channel][field] when the flag was explicitly provided.
func setContentField(cmd *cobra.Command, content map[string]map[string]string, flagName, channel, field, val string) {
	if cmd.Flags().Changed(flagName) {
		if content[channel] == nil {
			content[channel] = map[string]string{}
		}
		content[channel][field] = val
	}
}
