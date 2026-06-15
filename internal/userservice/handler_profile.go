// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package userservice

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

type updateContactsInput struct {
	Body struct {
		Contacts map[string]string `json:"contacts,omitempty" doc:"Contact addresses: address key (\"email\",\"phone\") -> address"`
	}
}

type userOutput struct {
	Body models.User
}

func (s *Server) registerProfileRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "get-profile",
		Method:      http.MethodGet,
		Path:        "/v1/users/me",
		Summary:     "Get current user profile",
		Tags:        []string{"Users"},
	}, func(ctx context.Context, input *struct{}) (*userOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		user, err := s.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		return &userOutput{Body: *user}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-contacts",
		Method:      http.MethodPut,
		Path:        "/v1/users/me/contacts",
		Summary:     "Update user contact information",
		Tags:        []string{"Users"},
	}, func(ctx context.Context, input *updateContactsInput) (*userOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		var email, phone *string
		if v, ok := input.Body.Contacts["email"]; ok {
			email = &v
		}
		if v, ok := input.Body.Contacts["phone"]; ok {
			phone = &v
		}
		if email == nil && phone == nil {
			return nil, huma.Error400BadRequest("at least one contact (email or phone) must be provided")
		}

		user, err := s.store.UpdateUserContacts(ctx, userID, email, phone)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		return &userOutput{Body: *user}, nil
	})
}
