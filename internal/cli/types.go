package cli

import "github.com/spf13/cobra"

func newTypesCmd() *cobra.Command {
	return &cobra.Command{Use: "types", Short: "Manage notification types"}
}
