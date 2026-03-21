package cli

import (
	"fmt"
	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Authentication operations"}
	cmd.AddCommand(newAuthTokenCmd())
	return cmd
}

func newAuthTokenCmd() *cobra.Command {
	var tenantID, userID string
	cmd := &cobra.Command{
		Use: "token", Short: "Exchange API key for a user JWT",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			resp, err := c.Auth.ExchangeToken(cmd.Context(), client.TokenRequest{TenantID: tenantID, UserID: userID})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if flagOutput == "json" {
				return printJSON(out, resp)
			}
			fmt.Fprintf(out, "Token: %s\nExpires: %s\n", resp.Token, resp.ExpiresAt)
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}
