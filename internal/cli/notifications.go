// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newNotificationsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "notifications", Aliases: []string{"notif"}, Short: "Send and inspect notifications"}
	cmd.AddCommand(newNotifSendCmd())
	cmd.AddCommand(newNotifStatusCmd())
	return cmd
}

func newNotifSendCmd() *cobra.Command {
	var organizationID, userID, email, phone, template, data, title, body, actionURL, actionLabel, idempotencyKey string
	var channels []string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			contacts := map[string]string{}
			if email != "" {
				contacts["email"] = email
			}
			if phone != "" {
				contacts["phone"] = phone
			}
			req := client.SendRequest{
				To: client.Recipient{
					OrganizationID: organizationID,
					UserID:   userID,
					Contacts: contacts,
				},
				Template: template,
				Channels: channels,
			}
			if len(contacts) == 0 {
				req.To.Contacts = nil
			}

			if title != "" || body != "" {
				req.Content = &client.Content{
					Title:       title,
					Body:        body,
					ActionURL:   actionURL,
					ActionLabel: actionLabel,
				}
			}

			if data != "" {
				if err := json.Unmarshal([]byte(data), &req.Data); err != nil {
					return fmt.Errorf("invalid --data JSON: %w", err)
				}
			}

			var opts []client.SendOption
			if idempotencyKey != "" {
				opts = append(opts, client.WithIdempotencyKey(idempotencyKey))
			}

			c := newClientFromCmd(cmd)
			resp, err := c.Notifications.Send(cmd.Context(), req, opts...)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, resp)
			}
			fmt.Fprintf(out, "%s %s\n", success("Accepted:"), bold(resp.NotificationID))
			return nil
		},
	}

	cmd.Flags().StringVar(&organizationID, "organization-id", "", "Organization ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email address override for this notification")
	cmd.Flags().StringVar(&phone, "phone", "", "Phone number override for this notification")
	cmd.Flags().StringVar(&template, "template", "", "Notification template slug")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Delivery channels (comma-separated)")
	cmd.Flags().StringVar(&data, "data", "", "Notification data as raw JSON")
	cmd.Flags().StringVar(&title, "title", "", "Content title")
	cmd.Flags().StringVar(&body, "body", "", "Content body")
	cmd.Flags().StringVar(&actionURL, "action-url", "", "Content action URL")
	cmd.Flags().StringVar(&actionLabel, "action-label", "", "Content action label")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key for deduplication")
	cmd.MarkFlagRequired("organization-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}

func newNotifStatusCmd() *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get the status of a notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			status, err := c.Notifications.GetStatus(cmd.Context(), id)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, status)
			}

			n := status.Notification
			fmt.Fprintf(out, "%s %s\n", bold("Notification"), bold(n.ID))
			fmt.Fprintf(out, "  %s   %s\n", label("Status:"), colorStatus(n.Status))
			fmt.Fprintf(out, "  %s    %s\n", label("Title:"), n.Title)
			fmt.Fprintf(out, "  %s     %s\n", label("Body:"), n.Body)
			fmt.Fprintf(out, "  %s %s\n", label("Channels:"), strings.Join(n.Channels, ", "))
			fmt.Fprintf(out, "  %s  %s\n", label("Created:"), fmtTime(n.CreatedAt))

			if len(status.Events) > 0 {
				fmt.Fprintln(out)
				var rows [][]string
				for _, e := range status.Events {
					detail := ""
					if msg, ok := e.Metadata["error"]; ok {
						detail = fmt.Sprintf("%v", msg)
					} else if msg, ok := e.Metadata["reason"]; ok {
						detail = fmt.Sprintf("%v", msg)
					}
					rows = append(rows, []string{e.Event, e.Channel, colorSeverity(e.Severity), detail, fmtTime(e.CreatedAt)})
				}
				printTable(out, []string{"EVENT", "CHANNEL", "SEVERITY", "DETAIL", "TIME"}, rows)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Notification ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}
