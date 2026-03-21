package cli

import (
	"fmt"
	"strings"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newTypesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "types", Short: "Manage notification types"}
	cmd.AddCommand(newTypesListCmd())
	cmd.AddCommand(newTypesCreateCmd())
	cmd.AddCommand(newTypesUpdateCmd())
	cmd.AddCommand(newTypesDeleteCmd())
	return cmd
}

func typeChannels(t client.NotificationType) string {
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

func newTypesListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all notification types",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			types, err := c.Types.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, types)
			}
			w := newTabWriter(out)
			printRow(w, "ID", "SLUG", "NAME", "GROUP_ID", "CHANNELS", "CREATED")
			for _, t := range types {
				printRow(w, t.ID, t.Slug, t.Name, t.GroupID, typeChannels(t), t.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			return w.Flush()
		},
	}
}

func newTypesCreateCmd() *cobra.Command {
	var groupID, slug, name string
	var emailSubject, emailBody, smsBody, inboxTitle, inboxBody string

	cmd := &cobra.Command{
		Use: "create", Short: "Create a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.CreateTypeRequest{
				GroupID: groupID,
				Slug:    slug,
				Name:    name,
			}
			setOptionalString(cmd, "email-subject", &req.EmailSubject, emailSubject)
			setOptionalString(cmd, "email-body", &req.EmailBody, emailBody)
			setOptionalString(cmd, "sms-body", &req.SMSBody, smsBody)
			setOptionalString(cmd, "inbox-title", &req.InboxTitle, inboxTitle)
			setOptionalString(cmd, "inbox-body", &req.InboxBody, inboxBody)

			c := newClientFromCmd(cmd)
			t, err := c.Types.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "Created type %s (%s)\n", t.ID, t.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&groupID, "group-id", "", "Group ID (required)")
	cmd.Flags().StringVar(&slug, "slug", "", "Type slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Type name (required)")
	cmd.Flags().StringVar(&emailSubject, "email-subject", "", "Email subject template")
	cmd.Flags().StringVar(&emailBody, "email-body", "", "Email body template")
	cmd.Flags().StringVar(&smsBody, "sms-body", "", "SMS body template")
	cmd.Flags().StringVar(&inboxTitle, "inbox-title", "", "Inbox title template")
	cmd.Flags().StringVar(&inboxBody, "inbox-body", "", "Inbox body template")
	cmd.MarkFlagRequired("group-id")
	cmd.MarkFlagRequired("slug")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newTypesUpdateCmd() *cobra.Command {
	var id, name string
	var emailSubject, emailBody, smsBody, inboxTitle, inboxBody string

	cmd := &cobra.Command{
		Use: "update", Short: "Update a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			var req client.UpdateTypeRequest
			if cmd.Flags().Changed("name") {
				req.Name = name
			}
			setOptionalString(cmd, "email-subject", &req.EmailSubject, emailSubject)
			setOptionalString(cmd, "email-body", &req.EmailBody, emailBody)
			setOptionalString(cmd, "sms-body", &req.SMSBody, smsBody)
			setOptionalString(cmd, "inbox-title", &req.InboxTitle, inboxTitle)
			setOptionalString(cmd, "inbox-body", &req.InboxBody, inboxBody)

			c := newClientFromCmd(cmd)
			t, err := c.Types.Update(cmd.Context(), id, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "Updated type %s\n", t.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Type ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Type name")
	cmd.Flags().StringVar(&emailSubject, "email-subject", "", "Email subject template")
	cmd.Flags().StringVar(&emailBody, "email-body", "", "Email body template")
	cmd.Flags().StringVar(&smsBody, "sms-body", "", "SMS body template")
	cmd.Flags().StringVar(&inboxTitle, "inbox-title", "", "Inbox title template")
	cmd.Flags().StringVar(&inboxBody, "inbox-body", "", "Inbox body template")
	cmd.MarkFlagRequired("id")
	return cmd
}

func newTypesDeleteCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "delete", Short: "Delete a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			if err := c.Types.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, map[string]string{"status": "deleted", "id": id})
			}
			fmt.Fprintf(out, "Deleted type %s\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Type ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}

// setOptionalString sets a *string pointer only when the flag was explicitly provided.
func setOptionalString(cmd *cobra.Command, flagName string, target **string, val string) {
	if cmd.Flags().Changed(flagName) {
		*target = &val
	}
}
