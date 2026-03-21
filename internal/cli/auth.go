package cli

import "github.com/spf13/cobra"

func newAuthCmd() *cobra.Command {
	return &cobra.Command{Use: "auth", Short: "Authentication operations"}
}
