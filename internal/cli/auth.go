// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
	var organizationID, userID string
	cmd := &cobra.Command{
		Use: "token", Short: "Exchange API key for a user JWT",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			resp, err := c.Auth.ExchangeToken(cmd.Context(), client.TokenRequest{OrganizationID: organizationID, UserID: userID})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, resp)
			}
			fmt.Fprintf(out, "Token: %s\nExpires: %s\n", resp.Token, resp.ExpiresAt)
			return nil
		},
	}
	cmd.Flags().StringVar(&organizationID, "organization-id", "", "Organization ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("organization-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}
