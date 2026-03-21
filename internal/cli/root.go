package cli

import (
	"fmt"
	"os"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

// resolveURL returns the base URL from environment variables or a default.
func resolveURL() string {
	if v := os.Getenv("HERMES_URL"); v != "" {
		return v
	}
	if v := os.Getenv("HERMES_ADMIN_URL"); v != "" {
		return v
	}
	return "http://localhost:8888"
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hermes",
		Short: "Hermes admin CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			apiKey, _ := cmd.Flags().GetString("api-key")
			if url == "" {
				return fmt.Errorf("--url or HERMES_URL is required")
			}
			if apiKey == "" {
				return fmt.Errorf("--api-key or HERMES_API_KEY is required")
			}
			return nil
		},
	}
	cmd.PersistentFlags().String("url", resolveURL(), "Hermes URL (env: HERMES_URL)")
	cmd.PersistentFlags().String("api-key", os.Getenv("HERMES_API_KEY"), "API key (env: HERMES_API_KEY)")
	cmd.PersistentFlags().StringP("output", "o", "table", "Output format: table or json")

	cmd.AddCommand(newGroupsCmd())
	cmd.AddCommand(newTypesCmd())
	cmd.AddCommand(newNotificationsCmd())
	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newInboxCmd())
	return cmd
}

// getOutput returns the output format flag value from the root command.
func getOutput(cmd *cobra.Command) string {
	val, _ := cmd.Root().PersistentFlags().GetString("output")
	return val
}

// newClientFromCmd creates a client from the root command's persistent flags.
func newClientFromCmd(cmd *cobra.Command) *client.Client {
	url, _ := cmd.Root().PersistentFlags().GetString("url")
	apiKey, _ := cmd.Root().PersistentFlags().GetString("api-key")
	return client.New(url, apiKey)
}

// NewRootCmdForTest returns the root command for testing.
func NewRootCmdForTest() *cobra.Command {
	return newRootCmd()
}

func Execute() error {
	return newRootCmd().Execute()
}
