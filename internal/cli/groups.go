package cli

import (
	"fmt"
	"strings"

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
			c := newClient()
			groups, err := c.Groups.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, groups)
			}
			w := newTabWriter(out)
			printRow(w, "ID", "SLUG", "NAME", "CHANNELS", "CREATED")
			for _, g := range groups {
				printRow(w, g.ID, g.Slug, g.Name, strings.Join(g.DefaultChannels, ","), g.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			return w.Flush()
		},
	}
}

func newGroupsCreateCmd() *cobra.Command {
	var slug, name string
	var channels []string
	cmd := &cobra.Command{
		Use: "create", Short: "Create a notification group",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			g, err := c.Groups.Create(cmd.Context(), client.CreateGroupRequest{Slug: slug, Name: name, DefaultChannels: channels})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, g)
			}
			fmt.Fprintf(out, "Created group %s (%s)\n", g.ID, g.Slug)
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
			c := newClient()
			req := client.UpdateGroupRequest{DefaultChannels: channels}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			g, err := c.Groups.Update(cmd.Context(), id, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, g)
			}
			fmt.Fprintf(out, "Updated group %s\n", g.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Group ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Group name")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Default channels")
	cmd.MarkFlagRequired("id")
	return cmd
}
