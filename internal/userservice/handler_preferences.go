// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package userservice

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type subscriptionIDInput struct {
	SubscriptionID string `path:"subscription_id" doc:"Subscription ID"`
}

type setPreferenceInput struct {
	SubscriptionID string `path:"subscription_id" doc:"Subscription ID"`
	Body           struct {
		OptedIn bool `json:"opted_in" required:"true" doc:"Whether the user is subscribed"`
	}
}

type preferenceSubscription struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	OptedIn    bool   `json:"opted_in"`
	Toggleable bool   `json:"toggleable"`
}

type preferenceCategory struct {
	ID              string                   `json:"id"`
	Slug            string                   `json:"slug"`
	Name            string                   `json:"name"`
	DefaultChannels []string                 `json:"default_channels"`
	DefaultState    string                   `json:"default_state"`
	Subscriptions   []preferenceSubscription `json:"subscriptions"`
}

type preferenceCenterOutput struct {
	Body struct {
		Categories []preferenceCategory `json:"categories"`
	}
}

type statusOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Operation result"`
	}
}

func (s *Server) registerPreferenceRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "get-preference-center",
		Method:      http.MethodGet,
		Path:        "/v1/users/me/preferences",
		Summary:     "Get notification preference center",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *struct{}) (*preferenceCenterOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		categories, err := s.store.ListCategories(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		userSubs, err := s.store.GetUserSubscriptions(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build lookup of user's explicit preferences
		userSubMap := make(map[string]bool)
		for _, us := range userSubs {
			userSubMap[us.SubscriptionID] = us.OptedIn
		}

		var result []preferenceCategory
		for _, cat := range categories {
			subs, err := s.store.ListSubscriptionsByCategory(ctx, cat.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("internal server error")
			}

			var prefSubs []preferenceSubscription
			for _, sub := range subs {
				optedIn := cat.DefaultState == "on" || cat.DefaultState == "required"
				if explicit, ok := userSubMap[sub.ID]; ok {
					optedIn = explicit
				}
				if cat.DefaultState == "required" {
					optedIn = true // required always on
				}

				prefSubs = append(prefSubs, preferenceSubscription{
					ID:         sub.ID,
					Slug:       sub.Slug,
					Name:       sub.Name,
					OptedIn:    optedIn,
					Toggleable: cat.DefaultState != "required",
				})
			}

			result = append(result, preferenceCategory{
				ID:              cat.ID,
				Slug:            cat.Slug,
				Name:            cat.Name,
				DefaultChannels: cat.DefaultChannels,
				DefaultState:    cat.DefaultState,
				Subscriptions:   prefSubs,
			})
		}

		resp := &preferenceCenterOutput{}
		resp.Body.Categories = result
		return resp, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "set-preference",
		Method:      http.MethodPut,
		Path:        "/v1/users/me/preferences/{subscription_id}",
		Summary:     "Set subscription preference",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *setPreferenceInput) (*statusOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		sub, err := s.store.GetSubscriptionByID(ctx, input.SubscriptionID)
		if err != nil {
			return nil, huma.Error404NotFound("subscription not found")
		}

		cat, err := s.store.GetCategoryByID(ctx, sub.CategoryID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if cat.DefaultState == "required" {
			return nil, huma.Error403Forbidden("cannot modify required subscription preferences")
		}

		if _, err := s.store.SetUserSubscription(ctx, userID, input.SubscriptionID, input.Body.OptedIn); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &statusOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "delete-preference",
		Method:      http.MethodDelete,
		Path:        "/v1/users/me/preferences/{subscription_id}",
		Summary:     "Delete subscription preference (revert to default)",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *subscriptionIDInput) (*statusOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		if err := s.store.DeleteUserSubscription(ctx, userID, input.SubscriptionID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &statusOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}
