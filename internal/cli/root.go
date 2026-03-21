package cli

import (
	"fmt"
	"os"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

var (
	flagURL    string
	flagAPIKey string
	flagOutput string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hermes",
		Short: "Hermes admin CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if flagURL == "" {
				return fmt.Errorf("--url or HERMES_ADMIN_URL is required")
			}
			if flagAPIKey == "" {
				return fmt.Errorf("--api-key or HERMES_API_KEY is required")
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&flagURL, "url", os.Getenv("HERMES_ADMIN_URL"), "Admin service base URL (env: HERMES_ADMIN_URL)")
	cmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", os.Getenv("HERMES_API_KEY"), "API key (env: HERMES_API_KEY)")
	cmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "table", "Output format: table or json")

	cmd.AddCommand(newGroupsCmd())
	cmd.AddCommand(newTypesCmd())
	cmd.AddCommand(newNotificationsCmd())
	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newInboxCmd())
	return cmd
}

func newClient() *client.Client {
	return client.New(flagURL, flagAPIKey)
}

// NewRootCmdForTest returns the root command for testing.
func NewRootCmdForTest() *cobra.Command {
	return newRootCmd()
}

func Execute() error {
	return newRootCmd().Execute()
}
