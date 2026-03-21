package cli

import "github.com/spf13/cobra"

func newGroupsCmd() *cobra.Command {
	return &cobra.Command{Use: "groups", Short: "Manage notification groups"}
}
