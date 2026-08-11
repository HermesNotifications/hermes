// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createSubscriptionInput struct {
	CategoryID string `path:"category_id" doc:"Category ID"`
	Body       struct {
		Slug      string `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name      string `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		SortOrder int    `json:"sort_order,omitempty" doc:"Display order within category"`
	}
}

type updateSubscriptionInput struct {
	ID   string `path:"id" doc:"Subscription ID"`
	Body struct {
		Name      string `json:"name" required:"true" doc:"Human-readable name"`
		SortOrder int    `json:"sort_order" doc:"Display order within category"`
	}
}

type subscriptionIDInput struct {
	ID string `path:"id" doc:"Subscription ID"`
}

type listSubscriptionsInput struct {
	CategoryID string `path:"category_id" doc:"Category ID"`
}

type subscriptionOutput struct {
	Body models.Subscription
}

type subscriptionListOutput struct {
	Body []models.Subscription
}

func (s *Server) registerSubscriptionRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-subscriptions",
		Method:      http.MethodGet,
		Path:        "/v1/subscriptions/categories/{category_id}/subscriptions",
		Summary:     "List subscriptions in a category",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *listSubscriptionsInput) (*subscriptionListOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		subs, err := s.store.ListSubscriptionsByCategory(ctx, input.CategoryID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &subscriptionListOutput{Body: subs}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-subscription",
		Method:        http.MethodPost,
		Path:          "/v1/subscriptions/categories/{category_id}/subscriptions",
		Summary:       "Create a subscription",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createSubscriptionInput) (*subscriptionOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		sub, err := s.store.CreateSubscription(ctx, input.CategoryID, input.Body.Slug, input.Body.Name, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &subscriptionOutput{Body: *sub}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-subscription",
		Method:      http.MethodPut,
		Path:        "/v1/subscriptions/{id}",
		Summary:     "Update a subscription",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *updateSubscriptionInput) (*subscriptionOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		sub, err := s.store.UpdateSubscription(ctx, input.ID, input.Body.Name, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			_ = s.cache.InvalidateSubscription(ctx, sub.ID)
		}
		return &subscriptionOutput{Body: *sub}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-subscription",
		Method:        http.MethodDelete,
		Path:          "/v1/subscriptions/{id}",
		Summary:       "Delete a subscription",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *subscriptionIDInput) (*struct{}, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		if err := s.store.DeleteSubscription(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			_ = s.cache.InvalidateSubscription(ctx, input.ID)
		}
		return nil, nil
	})
}
