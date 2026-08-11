// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newAPIKeysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "apikey", Short: "Manage API keys"}
	cmd.AddCommand(newAPIKeysListCmd())
	cmd.AddCommand(newAPIKeysCreateCmd())
	cmd.AddCommand(newAPIKeysRevokeCmd())
	return cmd
}

func newAPIKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			keys, err := c.APIKeys.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, keys)
			}
			var rows [][]string
			for _, k := range keys {
				rows = append(rows, []string{
					k.ID,
					bold(k.Name),
					strings.Join(k.Permissions, ","),
					fmtTime(k.CreatedAt.Format(time.RFC3339)),
				})
			}
			printTable(out, []string{"ID", "NAME", "PERMISSIONS", "CREATED"}, rows)
			return nil
		},
	}
}

func newAPIKeysCreateCmd() *cobra.Command {
	var name string
	var permissions []string
	cmd := &cobra.Command{
		Use: "create", Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			req := client.CreateAPIKeyRequest{Name: name}
			if len(permissions) > 0 {
				req.Permissions = permissions
			}
			k, err := c.APIKeys.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, k)
			}
			fmt.Fprintf(out, "ID:          %s\n", k.ID)
			fmt.Fprintf(out, "Name:        %s\n", k.Name)
			fmt.Fprintf(out, "API Key:     %s\n", bold(k.RawKey))
			fmt.Fprintf(out, "Permissions: %s\n", strings.Join(k.Permissions, ", "))
			fmt.Fprintf(out, "\n%s\n", dim("Store this key securely — it cannot be retrieved again."))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key name (required)")
	cmd.Flags().StringSliceVar(&permissions, "permissions", nil, "Permissions (comma-separated)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newAPIKeysRevokeCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "revoke", Short: "Revoke an API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			if err := c.APIKeys.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, map[string]string{"status": "revoked", "id": id})
			}
			fmt.Fprintf(out, "%s %s\n", success("Revoked key"), bold(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "API key ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}
