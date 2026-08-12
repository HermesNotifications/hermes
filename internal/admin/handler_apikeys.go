// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

// The rate limit fields are pointers throughout, and absent means "use the service
// default" — the same sentinel middleware.ResolveLimit applies to a zero override. That is
// the part callers get wrong, so it is spelled out on every field: omitting a limit asks
// for the default, it does not ask for zero. A minimum of 1 keeps a zero from reaching the
// database, where the CHECK constraint would turn a typo into a 500.
type createAPIKeyInput struct {
	Body struct {
		Name        string   `json:"name" required:"true" minLength:"1" doc:"Human-readable key name"`
		Permissions []string `json:"permissions,omitempty" doc:"Permission set (defaults to all except apikeys:manage)"`

		RateLimitPerSecond *int `json:"rate_limit_per_second,omitempty" minimum:"1" doc:"Sustained requests per second for this key. Omit to use the service default."`
		RateLimitBurst     *int `json:"rate_limit_burst,omitempty" minimum:"1" doc:"Requests admitted instantaneously for this key. Omit to use the service default."`
	}
}

// apiKeyView is the public shape of a key. The hash is never in it, and the raw secret is
// returned exactly once, by create.
type apiKeyView struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Permissions        []string  `json:"permissions"`
	CreatedAt          time.Time `json:"created_at"`
	RateLimitPerSecond *int      `json:"rate_limit_per_second,omitempty" doc:"Absent means the service default applies."`
	RateLimitBurst     *int      `json:"rate_limit_burst,omitempty" doc:"Absent means the service default applies."`
}

func toAPIKeyView(k *models.APIKey) apiKeyView {
	return apiKeyView{
		ID:                 k.ID,
		Name:               k.Name,
		Permissions:        k.Permissions,
		CreatedAt:          k.CreatedAt,
		RateLimitPerSecond: k.RateLimitPerSecond,
		RateLimitBurst:     k.RateLimitBurst,
	}
}

type apiKeyCreatedOutput struct {
	Body struct {
		ID                 string    `json:"id"`
		Name               string    `json:"name"`
		RawKey             string    `json:"raw_key"`
		Permissions        []string  `json:"permissions"`
		CreatedAt          time.Time `json:"created_at"`
		RateLimitPerSecond *int      `json:"rate_limit_per_second,omitempty"`
		RateLimitBurst     *int      `json:"rate_limit_burst,omitempty"`
	}
}

type listAPIKeysOutput struct {
	Body []apiKeyView
}

type deleteAPIKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

// setAPIKeyRateLimitInput is a PUT on the whole limit, not a PATCH of its parts.
//
// JSON cannot distinguish an absent field from an explicit null once it is unmarshalled
// into a pointer, so a PATCH could never tell "leave this alone" from "clear this". Making
// the endpoint a replacement removes the ambiguity: what you send is what the key has, and
// an empty body resets it to the service default.
type setAPIKeyRateLimitInput struct {
	ID   string `path:"id" doc:"API key ID"`
	Body struct {
		PerSecond *int `json:"per_second,omitempty" minimum:"1" doc:"Sustained requests per second. Omit to reset to the service default."`
		Burst     *int `json:"burst,omitempty" minimum:"1" doc:"Requests admitted instantaneously. Omit to reset to the service default."`
	}
}

type apiKeyOutput struct {
	Body apiKeyView
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
			out.Body = append(out.Body, toAPIKeyView(&k))
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

		limits := models.RateLimitOverride{
			PerSecond: input.Body.RateLimitPerSecond,
			Burst:     input.Body.RateLimitBurst,
		}

		k, err := s.store.CreateAPIKey(ctx, keyID, keyHash, input.Body.Name, permissions, limits)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to create api key")
		}

		out := &apiKeyCreatedOutput{}
		out.Body.ID = k.ID
		out.Body.Name = k.Name
		out.Body.RawKey = rawKey
		out.Body.RateLimitPerSecond = k.RateLimitPerSecond
		out.Body.RateLimitBurst = k.RateLimitBurst
		out.Body.Permissions = k.Permissions
		out.Body.CreatedAt = k.CreatedAt
		return out, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "set-api-key-rate-limit",
		Method:      http.MethodPut,
		Path:        "/v1/apikeys/{id}/rate-limit",
		Summary:     "Set or clear a key's rate limit",
		Description: "Replaces this key's rate limit. Omitted fields reset to the service default, " +
			"so an empty body clears the override entirely. Takes effect within the API key " +
			"cache TTL; this endpoint invalidates that entry so the change is immediate.",
		Tags: []string{"API Keys"},
	}, func(ctx context.Context, input *setAPIKeyRateLimitInput) (*apiKeyOutput, error) {
		if err := requirePermission(ctx, auth.PermAPIKeysManage); err != nil {
			return nil, err
		}

		k, err := s.store.UpdateAPIKeyRateLimits(ctx, input.ID, models.RateLimitOverride{
			PerSecond: input.Body.PerSecond,
			Burst:     input.Body.Burst,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to update api key rate limit")
		}
		if k == nil {
			return nil, huma.Error404NotFound("api key not found")
		}

		// Without this the new limit would not apply until the cached key record expired,
		// up to five minutes later — long enough for an operator throttling an abusive
		// caller to conclude the endpoint does not work and try something worse.
		if s.cache != nil {
			_ = s.cache.InvalidateAPIKey(ctx, input.ID)
		}

		// The limiter pins a caller's limit when its bucket is created, so an in-flight
		// bucket keeps the old rate until it ages out (30 minutes idle). Invalidating the
		// cache is what makes the change take effect for the next new bucket; it does not
		// retune one already in use.
		return &apiKeyOutput{Body: toAPIKeyView(k)}, nil
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
