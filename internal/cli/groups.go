package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "groups", Short: "Manage notification groups"}
	cmd.AddCommand(newGroupsListCmd())
	cmd.AddCommand(newGroupsCreateCmd())
	cmd.AddCommand(newGroupsUpdateCmd())
	return cmd
}

func newGroupsListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all notification groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			groups, err := c.Groups.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, groups)
			}
			var rows [][]string
			for _, g := range groups {
				rows = append(rows, []string{g.ID, g.Slug, bold(g.Name), strings.Join(g.DefaultChannels, ","), fmtTime(g.CreatedAt.Format(time.RFC3339))})
			}
			printTable(out, []string{"ID", "SLUG", "NAME", "CHANNELS", "CREATED"}, rows)
			return nil
		},
	}
}

func newGroupsCreateCmd() *cobra.Command {
	var slug, name string
	var channels []string
	cmd := &cobra.Command{
		Use: "create", Short: "Create a notification group",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			g, err := c.Groups.Create(cmd.Context(), client.CreateGroupRequest{Slug: slug, Name: name, DefaultChannels: channels})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, g)
			}
			fmt.Fprintf(out, "%s %s %s\n", success("Created group"), bold(g.ID), dim("("+g.Slug+")"))
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "Group slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Group name (required)")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Default channels (comma-separated)")
	cmd.MarkFlagRequired("slug")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newGroupsUpdateCmd() *cobra.Command {
	var id, name string
	var channels []string
	cmd := &cobra.Command{
		Use: "update", Short: "Update a notification group",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			req := client.UpdateGroupRequest{DefaultChannels: channels}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			g, err := c.Groups.Update(cmd.Context(), id, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, g)
			}
			fmt.Fprintf(out, "%s %s\n", success("Updated group"), bold(g.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Group ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Group name")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Default channels")
	cmd.MarkFlagRequired("id")
	return cmd
}
