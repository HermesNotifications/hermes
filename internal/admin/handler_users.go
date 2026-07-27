// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type listUsersInput struct {
	OrganizationID string `query:"organization_id" doc:"Filter by organization ID"`
}

type userItem struct {
	ID               string            `json:"id"`
	OrganizationID   string            `json:"organization_id"`
	ExternalID       string            `json:"external_id"`
	Contacts         map[string]string `json:"contacts,omitempty"`
	Locale           *string           `json:"locale"`
	OrganizationName string            `json:"organization_name"`
	CreatedAt        time.Time         `json:"created_at"`
}

type userListOutput struct {
	Body []userItem
}

func (s *Server) registerUserRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/v1/users",
		Summary:     "List users",
		Tags:        []string{"Users"},
	}, func(ctx context.Context, input *listUsersInput) (*userListOutput, error) {
		users, err := s.store.ListUsers(ctx, input.OrganizationID)
		if err != nil {
			s.logger.Error("failed to list users", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build organization name lookup
		organizations, err := s.store.ListOrganizations(ctx)
		if err != nil {
			s.logger.Error("failed to list organizations for name lookup", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		organizationNames := make(map[string]string, len(organizations))
		for _, t := range organizations {
			organizationNames[t.ID] = t.Name
		}

		items := make([]userItem, len(users))
		for i, u := range users {
			items[i] = userItem{
				ID:               u.ID,
				OrganizationID:   u.OrganizationID,
				ExternalID:       u.ExternalID,
				Contacts:         u.Contacts,
				Locale:           u.Locale,
				OrganizationName: organizationNames[u.OrganizationID],
				CreatedAt:        u.CreatedAt,
			}
		}
		return &userListOutput{Body: items}, nil
	})
}
