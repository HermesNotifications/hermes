// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createCategoryInput struct {
	Body struct {
		Slug            string   `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name            string   `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels,omitempty" doc:"Default delivery channels"`
		DefaultState    string   `json:"default_state" required:"true" enum:"on,off,required" doc:"Default subscription state"`
		SortOrder       int      `json:"sort_order,omitempty" doc:"Display order"`
	}
}

type updateCategoryInput struct {
	ID   string `path:"id" doc:"Category ID"`
	Body struct {
		Name            string   `json:"name" required:"true" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels" doc:"Default delivery channels"`
		DefaultState    string   `json:"default_state" required:"true" enum:"on,off,required" doc:"Default subscription state"`
		SortOrder       int      `json:"sort_order" doc:"Display order"`
	}
}

type categoryIDInput struct {
	ID string `path:"id" doc:"Category ID"`
}

type categoryOutput struct {
	Body models.SubscriptionCategory
}

type categoryListOutput struct {
	Body []models.SubscriptionCategory
}

func (s *Server) registerCategoryRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-subscription-categories",
		Method:      http.MethodGet,
		Path:        "/v1/subscriptions/categories",
		Summary:     "List subscription categories",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *struct{}) (*categoryListOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		categories, err := s.store.ListCategories(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &categoryListOutput{Body: categories}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-subscription-category",
		Method:        http.MethodPost,
		Path:          "/v1/subscriptions/categories",
		Summary:       "Create a subscription category",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createCategoryInput) (*categoryOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		c, err := s.store.CreateCategory(ctx, input.Body.Slug, input.Body.Name, input.Body.DefaultChannels, input.Body.DefaultState, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &categoryOutput{Body: *c}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-subscription-category",
		Method:      http.MethodPut,
		Path:        "/v1/subscriptions/categories/{id}",
		Summary:     "Update a subscription category",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *updateCategoryInput) (*categoryOutput, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		c, err := s.store.UpdateCategory(ctx, input.ID, input.Body.Name, input.Body.DefaultChannels, input.Body.DefaultState, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			_ = s.cache.InvalidateCategory(ctx, c.ID)
		}
		return &categoryOutput{Body: *c}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-subscription-category",
		Method:        http.MethodDelete,
		Path:          "/v1/subscriptions/categories/{id}",
		Summary:       "Delete a subscription category",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *categoryIDInput) (*struct{}, error) {
		if err := requirePermission(ctx, auth.PermTemplatesManage); err != nil {
			return nil, err
		}

		if err := s.store.DeleteCategory(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			_ = s.cache.InvalidateCategory(ctx, input.ID)
		}
		return nil, nil
	})
}
