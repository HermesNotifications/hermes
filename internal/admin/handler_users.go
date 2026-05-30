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
	TenantID string `query:"tenant_id" doc:"Filter by tenant ID"`
}

type userItem struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ExternalID string    `json:"external_id"`
	Email      *string   `json:"email"`
	Phone      *string   `json:"phone"`
	Locale     *string   `json:"locale"`
	TenantName string    `json:"tenant_name"`
	CreatedAt  time.Time `json:"created_at"`
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
		users, err := s.store.ListUsers(ctx, input.TenantID)
		if err != nil {
			s.logger.Error("failed to list users", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build tenant name lookup
		tenants, err := s.store.ListTenants(ctx)
		if err != nil {
			s.logger.Error("failed to list tenants for name lookup", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		tenantNames := make(map[string]string, len(tenants))
		for _, t := range tenants {
			tenantNames[t.ID] = t.Name
		}

		items := make([]userItem, len(users))
		for i, u := range users {
			items[i] = userItem{
				ID:         u.ID,
				TenantID:   u.TenantID,
				ExternalID: u.ExternalID,
				Email:      u.Email,
				Phone:      u.Phone,
				Locale:     u.Locale,
				TenantName: tenantNames[u.TenantID],
				CreatedAt:  u.CreatedAt,
			}
		}
		return &userListOutput{Body: items}, nil
	})
}
