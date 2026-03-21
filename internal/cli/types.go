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
			c := newClient()
			types, err := c.Types.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
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
	var req client.CreateTypeRequest
	cmd := &cobra.Command{
		Use: "create", Short: "Create a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			req.GroupID = groupID
			req.Slug = slug
			req.Name = name
			c := newClient()
			t, err := c.Types.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "Created type %s (%s)\n", t.ID, t.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&groupID, "group-id", "", "Group ID (required)")
	cmd.Flags().StringVar(&slug, "slug", "", "Type slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Type name (required)")
	cmd.MarkFlagRequired("group-id")
	cmd.MarkFlagRequired("slug")
	cmd.MarkFlagRequired("name")
	addOptionalStringFlag(cmd, &req.EmailSubject, "email-subject", "Email subject template")
	addOptionalStringFlag(cmd, &req.EmailBody, "email-body", "Email body template")
	addOptionalStringFlag(cmd, &req.SMSBody, "sms-body", "SMS body template")
	addOptionalStringFlag(cmd, &req.InboxTitle, "inbox-title", "Inbox title template")
	addOptionalStringFlag(cmd, &req.InboxBody, "inbox-body", "Inbox body template")
	return cmd
}

func newTypesUpdateCmd() *cobra.Command {
	var id, name string
	var req client.UpdateTypeRequest
	cmd := &cobra.Command{
		Use: "update", Short: "Update a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("name") {
				req.Name = name
			}
			c := newClient()
			t, err := c.Types.Update(cmd.Context(), id, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, t)
			}
			fmt.Fprintf(out, "Updated type %s\n", t.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Type ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Type name")
	cmd.MarkFlagRequired("id")
	addOptionalStringFlag(cmd, &req.EmailSubject, "email-subject", "Email subject template")
	addOptionalStringFlag(cmd, &req.EmailBody, "email-body", "Email body template")
	addOptionalStringFlag(cmd, &req.SMSBody, "sms-body", "SMS body template")
	addOptionalStringFlag(cmd, &req.InboxTitle, "inbox-title", "Inbox title template")
	addOptionalStringFlag(cmd, &req.InboxBody, "inbox-body", "Inbox body template")
	return cmd
}

func newTypesDeleteCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "delete", Short: "Delete a notification type",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.Types.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, map[string]string{"status": "deleted", "id": id})
			}
			w := newTabWriter(out)
			printRow(w, fmt.Sprintf("Deleted type %s", id))
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Type ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}

func addOptionalStringFlag(cmd *cobra.Command, p **string, name, usage string) {
	var val string
	cmd.Flags().StringVar(&val, name, "", usage)
	old := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed(name) {
			*p = &val
		}
		if old != nil {
			return old(cmd, args)
		}
		return nil
	}
}
