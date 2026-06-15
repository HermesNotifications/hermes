// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createTemplateInput struct {
	Body struct {
		Slug            string                       `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name            string                       `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		SubscriptionID  *string                      `json:"subscription_id,omitempty" doc:"Subscription ID (null for standalone)"`
		DefaultChannels []string                     `json:"default_channels,omitempty" doc:"Default channels (used when no subscription)"`
		Content         map[string]map[string]string `json:"content,omitempty" doc:"Per-channel content: channel slug -> field key -> template string (e.g. {\"email\":{\"subject\":\"...\",\"body\":\"...\"}})"`
	}
}

type updateTemplateInput struct {
	ID   string `path:"id" doc:"Template ID"`
	Body struct {
		Name            string                       `json:"name" doc:"Human-readable name"`
		SubscriptionID  *string                      `json:"subscription_id,omitempty" doc:"Subscription ID (null for standalone)"`
		DefaultChannels []string                     `json:"default_channels,omitempty" doc:"Default channels (used when no subscription)"`
		Content         map[string]map[string]string `json:"content,omitempty" doc:"Per-channel content: channel slug -> field key -> template string (e.g. {\"email\":{\"subject\":\"...\",\"body\":\"...\"}})"`
	}
}

type deleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type templateOutput struct {
	Body models.NotificationTemplate
}

type templateListOutput struct {
	Body []models.NotificationTemplate
}

func (s *Server) registerTemplateRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-templates",
		Method:      http.MethodGet,
		Path:        "/v1/templates",
		Summary:     "List notification templates",
		Tags:        []string{"Templates"},
	}, func(ctx context.Context, input *struct{}) (*templateListOutput, error) {
		templates, err := s.store.ListTemplates(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &templateListOutput{Body: templates}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-template",
		Method:        http.MethodPost,
		Path:          "/v1/templates",
		Summary:       "Create a notification template",
		Tags:          []string{"Templates"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createTemplateInput) (*templateOutput, error) {
		nt := &models.NotificationTemplate{
			Slug: input.Body.Slug, Name: input.Body.Name,
			SubscriptionID: input.Body.SubscriptionID, DefaultChannels: input.Body.DefaultChannels,
			Content: input.Body.Content,
		}
		nt, err := s.store.CreateTemplate(ctx, nt)
		if err != nil {
			s.logger.Error("failed to create template", "error", err, "slug", input.Body.Slug)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &templateOutput{Body: *nt}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-template",
		Method:      http.MethodPut,
		Path:        "/v1/templates/{id}",
		Summary:     "Update a notification template",
		Tags:        []string{"Templates"},
	}, func(ctx context.Context, input *updateTemplateInput) (*templateOutput, error) {
		nt := &models.NotificationTemplate{
			ID: input.ID, Name: input.Body.Name,
			SubscriptionID: input.Body.SubscriptionID, DefaultChannels: input.Body.DefaultChannels,
			Content: input.Body.Content,
		}
		nt, err := s.store.UpdateTemplate(ctx, nt)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Invalidate cache
		if s.cache != nil {
			_ = s.cache.InvalidateTemplateConfig(ctx, nt.Slug)
		}
		return &templateOutput{Body: *nt}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-template",
		Method:        http.MethodDelete,
		Path:          "/v1/templates/{id}",
		Summary:       "Delete a notification template",
		Tags:          []string{"Templates"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deleteTemplateInput) (*struct{}, error) {
		// Get slug for cache invalidation before deleting
		existing, err := s.store.GetTemplateByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if err := s.store.DeleteTemplate(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			_ = s.cache.InvalidateTemplateConfig(ctx, existing.Slug)
		}
		return nil, nil
	})
}
