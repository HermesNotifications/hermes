// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/hermesnotifications/hermes/internal/auth"
)

type organizationItem struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DefaultLocale string    `json:"default_locale"`
	UserCount     int       `json:"user_count"`
	CreatedAt     time.Time `json:"created_at"`
}

type organizationListOutput struct {
	Body []organizationItem
}

type createOrganizationInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" doc:"Organization name"`
	}
}

type createOrganizationOutput struct {
	Body organizationItem
}

func (s *Server) registerOrganizationRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-organizations",
		Method:      http.MethodGet,
		Path:        "/v1/organizations",
		Summary:     "List organizations",
		Tags:        []string{"Organizations"},
	}, func(ctx context.Context, input *struct{}) (*organizationListOutput, error) {
		if err := requirePermission(ctx, auth.PermOrganizationsManage); err != nil {
			return nil, err
		}

		organizations, err := s.store.ListOrganizations(ctx)
		if err != nil {
			s.logger.Error("failed to list organizations", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		counts, err := s.store.CountUsersByOrganization(ctx)
		if err != nil {
			s.logger.Error("failed to count users by organization", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		items := make([]organizationItem, len(organizations))
		for i, t := range organizations {
			items[i] = organizationItem{
				ID:            t.ID,
				Name:          t.Name,
				DefaultLocale: t.DefaultLocale,
				UserCount:     counts[t.ID],
				CreatedAt:     t.CreatedAt,
			}
		}
		return &organizationListOutput{Body: items}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-organization",
		Method:        http.MethodPost,
		Path:          "/v1/organizations",
		Summary:       "Create an organization",
		Tags:          []string{"Organizations"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createOrganizationInput) (*createOrganizationOutput, error) {
		if err := requirePermission(ctx, auth.PermOrganizationsManage); err != nil {
			return nil, err
		}

		id := uuid.New().String()
		organization, err := s.store.CreateOrganization(ctx, id, input.Body.Name)
		if err != nil {
			s.logger.Error("failed to create organization", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &createOrganizationOutput{Body: organizationItem{
			ID:            organization.ID,
			Name:          organization.Name,
			DefaultLocale: organization.DefaultLocale,
			UserCount:     0,
			CreatedAt:     organization.CreatedAt,
		}}, nil
	})
}
