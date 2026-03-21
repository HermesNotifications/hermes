package cli

import "github.com/spf13/cobra"

func newInboxCmd() *cobra.Command {
	return &cobra.Command{Use: "inbox", Short: "Inbox operations"}
}
