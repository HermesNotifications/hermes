package cli

import (
	"encoding/json"
	"fmt"

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
	var tenantID, userID, notifType, group, data, title, body, actionURL, actionLabel, idempotencyKey string
	var channels []string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.SendRequest{
				TenantID: tenantID,
				UserID:   userID,
				Type:     notifType,
				Group:    group,
				Channels: channels,
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
			fmt.Fprintf(out, "Accepted: %s\n", resp.NotificationID)
			return nil
		},
	}

	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&notifType, "type", "", "Notification type")
	cmd.Flags().StringVar(&group, "group", "", "Notification group")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Delivery channels (comma-separated)")
	cmd.Flags().StringVar(&data, "data", "", "Notification data as raw JSON")
	cmd.Flags().StringVar(&title, "title", "", "Content title")
	cmd.Flags().StringVar(&body, "body", "", "Content body")
	cmd.Flags().StringVar(&actionURL, "action-url", "", "Content action URL")
	cmd.Flags().StringVar(&actionLabel, "action-label", "", "Content action label")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key for deduplication")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}

func newNotifStatusCmd() *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get the status of a notification",
		// Always outputs JSON — the response contains nested notification and events
		// structures as json.RawMessage, which don't have a meaningful table format.
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			status, err := c.Notifications.GetStatus(cmd.Context(), id)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			return printJSON(out, status)
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Notification ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}
