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

func newCategoriesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "categories", Short: "Manage subscription categories"}
	cmd.AddCommand(newCategoriesListCmd())
	cmd.AddCommand(newCategoriesCreateCmd())
	cmd.AddCommand(newCategoriesUpdateCmd())
	cmd.AddCommand(newCategoriesDeleteCmd())
	return cmd
}

func newCategoriesListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all subscription categories",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			categories, err := c.Categories.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, categories)
			}
			var rows [][]string
			for _, cat := range categories {
				rows = append(rows, []string{cat.ID, cat.Slug, bold(cat.Name), strings.Join(cat.DefaultChannels, ","), cat.DefaultState, fmtTime(cat.CreatedAt.Format(time.RFC3339))})
			}
			printTable(out, []string{"ID", "SLUG", "NAME", "CHANNELS", "STATE", "CREATED"}, rows)
			return nil
		},
	}
}

func newCategoriesCreateCmd() *cobra.Command {
	var slug, name, defaultState string
	var channels []string
	var sortOrder int
	cmd := &cobra.Command{
		Use: "create", Short: "Create a subscription category",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			cat, err := c.Categories.Create(cmd.Context(), client.CreateCategoryRequest{
				Slug: slug, Name: name, DefaultChannels: channels, DefaultState: defaultState, SortOrder: sortOrder,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, cat)
			}
			fmt.Fprintf(out, "%s %s %s\n", success("Created category"), bold(cat.ID), dim("("+cat.Slug+")"))
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "Category slug (required)")
	cmd.Flags().StringVar(&name, "name", "", "Category name (required)")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Default channels (comma-separated)")
	cmd.Flags().StringVar(&defaultState, "default-state", "on", "Default state: on, off, required")
	cmd.Flags().IntVar(&sortOrder, "sort-order", 0, "Sort order")
	cmd.MarkFlagRequired("slug")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newCategoriesUpdateCmd() *cobra.Command {
	var id, name, defaultState string
	var channels []string
	var sortOrder int
	cmd := &cobra.Command{
		Use: "update", Short: "Update a subscription category",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			cat, err := c.Categories.Update(cmd.Context(), id, client.UpdateCategoryRequest{
				Name: name, DefaultChannels: channels, DefaultState: defaultState, SortOrder: sortOrder,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, cat)
			}
			fmt.Fprintf(out, "%s %s\n", success("Updated category"), bold(cat.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Category ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Category name")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "Default channels")
	cmd.Flags().StringVar(&defaultState, "default-state", "on", "Default state: on, off, required")
	cmd.Flags().IntVar(&sortOrder, "sort-order", 0, "Sort order")
	cmd.MarkFlagRequired("id")
	return cmd
}

func newCategoriesDeleteCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "delete", Short: "Delete a subscription category",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			if err := c.Categories.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, map[string]string{"status": "deleted", "id": id})
			}
			fmt.Fprintf(out, "%s %s\n", success("Deleted category"), bold(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Category ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}
