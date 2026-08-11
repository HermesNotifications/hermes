// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package userservice

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/provider"
)

var (
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRe = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
)

// validateContact checks an address value for a known address key. Keys without
// a specific format (future address types) require only a non-empty value.
func validateContact(key, address string) error {
	switch key {
	case "email":
		if !emailRe.MatchString(address) {
			return fmt.Errorf("invalid email address")
		}
	case "phone":
		if !phoneRe.MatchString(address) {
			return fmt.Errorf("invalid phone number")
		}
	default:
		if strings.TrimSpace(address) == "" {
			return fmt.Errorf("empty address")
		}
	}
	return nil
}

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

		if len(input.Body.Contacts) == 0 {
			return nil, huma.Error400BadRequest("at least one contact must be provided")
		}
		// Validate every key against the channel registry's known address keys and
		// the per-key value format before persisting (no arbitrary keys/values).
		for key, address := range input.Body.Contacts {
			if !provider.Builtins.IsAddressKey(key) {
				return nil, huma.Error400BadRequest("unsupported contact key: " + key)
			}
			if err := validateContact(key, address); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
		}
		for key, address := range input.Body.Contacts {
			if err := s.store.SetUserContact(ctx, userID, key, address); err != nil {
				return nil, huma.Error500InternalServerError("internal server error")
			}
		}

		user, err := s.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		return &userOutput{Body: *user}, nil
	})
}
