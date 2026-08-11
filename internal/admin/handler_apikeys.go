// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/jackc/pgx/v5"
)

type createAPIKeyInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" doc:"Human-readable key name"`
		// Required: a key that names no organization can act for all of them, which
		// is the hole ADR 0011 closes. Existing keys may still have none, but nothing
		// mints another one.
		OrganizationID string   `json:"organization_id" required:"true" minLength:"1" doc:"Organization this key may act for"`
		Permissions    []string `json:"permissions,omitempty" doc:"Permission set (defaults to all except apikeys:manage)"`
	}
}

type apiKeyCreatedOutput struct {
	Body struct {
		ID             string    `json:"id"`
		Name           string    `json:"name"`
		OrganizationID string    `json:"organization_id"`
		RawKey         string    `json:"raw_key"`
		Permissions    []string  `json:"permissions"`
		CreatedAt      time.Time `json:"created_at"`
	}
}

type listAPIKeysOutput struct {
	Body []struct {
		ID             string    `json:"id"`
		Name           string    `json:"name"`
		OrganizationID string    `json:"organization_id,omitempty"`
		Permissions    []string  `json:"permissions"`
		CreatedAt      time.Time `json:"created_at"`
	}
}

type deleteAPIKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

func (s *Server) registerAPIKeyRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-api-keys",
		Method:      http.MethodGet,
		Path:        "/v1/apikeys",
		Summary:     "List all API keys",
		Tags:        []string{"API Keys"},
	}, func(ctx context.Context, input *struct{}) (*listAPIKeysOutput, error) {
		// Finding 3. Previously `if key != nil && !HasPermission(...)`, which PASSES
		// when the key is nil — a nil key was granted every permission. Unreachable in
		// production because APIKeyMiddleware 401s first, but fail-open by construction.
		if err := requirePermission(ctx, auth.PermAPIKeysManage); err != nil {
			return nil, err
		}

		keys, err := s.store.ListAPIKeys(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list api keys")
		}
		out := &listAPIKeysOutput{}
		for _, k := range keys {
			out.Body = append(out.Body, struct {
				ID             string    `json:"id"`
				Name           string    `json:"name"`
				OrganizationID string    `json:"organization_id,omitempty"`
				Permissions    []string  `json:"permissions"`
				CreatedAt      time.Time `json:"created_at"`
			}{
				ID:             k.ID,
				Name:           k.Name,
				OrganizationID: k.OrganizationID,
				Permissions:    k.Permissions,
				CreatedAt:      k.CreatedAt,
			})
		}
		return out, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-api-key",
		Method:        http.MethodPost,
		Path:          "/v1/apikeys",
		Summary:       "Create a new API key",
		Tags:          []string{"API Keys"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createAPIKeyInput) (*apiKeyCreatedOutput, error) {
		// Finding 3. Previously `if key != nil && !HasPermission(...)`, which PASSES
		// when the key is nil — a nil key was granted every permission. Unreachable in
		// production because APIKeyMiddleware 401s first, but fail-open by construction.
		if err := requirePermission(ctx, auth.PermAPIKeysManage); err != nil {
			return nil, err
		}

		permissions := input.Body.Permissions
		if len(permissions) == 0 {
			permissions = auth.DefaultPermissions
		}

		if err := auth.ValidatePermissions(permissions); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}

		rawKey, keyID, err := auth.GenerateAPIKey("")
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to generate api key")
		}

		_, secret, err := auth.ParseAPIKey(rawKey)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to parse generated key")
		}

		keyHash := auth.HMACHashAPIKey(secret, s.hmacSecret)

		// Reject an unknown organization here rather than letting the foreign key
		// surface as a 500. A caller who mistypes an organization ID should be told
		// which input was wrong.
		org, err := s.organizations.GetOrganizationByID(ctx, input.Body.OrganizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows), err == nil && org == nil:
			return nil, huma.Error400BadRequest("organization not found")
		case err != nil:
			return nil, huma.Error500InternalServerError("failed to look up organization")
		}

		k, err := s.store.CreateAPIKey(ctx, keyID, keyHash, input.Body.Name, input.Body.OrganizationID, permissions)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to create api key")
		}

		out := &apiKeyCreatedOutput{}
		out.Body.ID = k.ID
		out.Body.Name = k.Name
		out.Body.OrganizationID = k.OrganizationID
		out.Body.RawKey = rawKey
		out.Body.Permissions = k.Permissions
		out.Body.CreatedAt = k.CreatedAt
		return out, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-api-key",
		Method:        http.MethodDelete,
		Path:          "/v1/apikeys/{id}",
		Summary:       "Revoke an API key",
		Tags:          []string{"API Keys"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deleteAPIKeyInput) (*struct{}, error) {
		// Finding 3. Previously `if key != nil && !HasPermission(...)`, which PASSES
		// when the key is nil — a nil key was granted every permission. Unreachable in
		// production because APIKeyMiddleware 401s first, but fail-open by construction.
		if err := requirePermission(ctx, auth.PermAPIKeysManage); err != nil {
			return nil, err
		}

		// Prevent self-deletion
		validated := auth.GetValidatedKey(ctx)
		if validated != nil && validated.ID == input.ID {
			return nil, huma.Error400BadRequest("cannot revoke the key used to authenticate this request")
		}

		if err := s.store.DeleteAPIKey(ctx, input.ID); err != nil {
			return nil, huma.Error404NotFound("api key not found")
		}

		if s.cache != nil {
			_ = s.cache.InvalidateAPIKey(ctx, input.ID)
		}

		return nil, nil
	})
}
