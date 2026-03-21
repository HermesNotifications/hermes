package cli

import "github.com/spf13/cobra"

func newNotificationsCmd() *cobra.Command {
	return &cobra.Command{Use: "notifications", Aliases: []string{"notif"}, Short: "Send and inspect notifications"}
}
