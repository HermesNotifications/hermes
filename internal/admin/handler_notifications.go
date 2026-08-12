// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermesnotifications/hermes/internal/auth"
	"github.com/hermesnotifications/hermes/internal/models"
)

type listNotificationsInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"200" doc:"Max results (default 50)"`
}

type notificationItem struct {
	ID             string                    `json:"id"`
	OrganizationID string                    `json:"organization_id"`
	UserID         string                    `json:"user_id"`
	TemplateID     *string                   `json:"template_id,omitempty"`
	TemplateSlug   string                    `json:"template_slug,omitempty"`
	CategoryID     string                    `json:"category_id"`
	Title          string                    `json:"title"`
	Body           string                    `json:"body"`
	Channels       []string                  `json:"channels"`
	Status         models.NotificationStatus `json:"status"`
	CreatedAt      time.Time                 `json:"created_at"`
}

type notificationListOutput struct {
	Body []notificationItem
}

type getNotificationInput struct {
	ID string `path:"id" doc:"Notification ID"`
}

type notificationStatusOutput struct {
	Body struct {
		Notification models.Notification        `json:"notification" doc:"The notification record"`
		Events       []models.NotificationEvent `json:"events" doc:"Timeline of notification events"`
	}
}

func (s *Server) registerNotificationRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-notifications",
		Method:      http.MethodGet,
		Path:        "/v1/notifications",
		Summary:     "List recent notifications",
		Tags:        []string{"Notifications"},
	}, func(ctx context.Context, input *listNotificationsInput) (*notificationListOutput, error) {
		if err := requirePermission(ctx, auth.PermOrganizationsManage); err != nil {
			return nil, err
		}

		limit := input.Limit
		if limit <= 0 {
			limit = 50
		}
		notifications, err := s.store.ListRecentNotifications(ctx, limit)
		if err != nil {
			s.logger.Error("failed to list recent notifications", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build template slug lookup
		templates, err := s.store.ListTemplates(ctx)
		if err != nil {
			s.logger.Error("failed to list templates for slug lookup", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		templateSlugs := make(map[string]string, len(templates))
		for _, t := range templates {
			templateSlugs[t.ID] = t.Slug
		}

		items := make([]notificationItem, len(notifications))
		for i, n := range notifications {
			items[i] = notificationItem{
				ID:             n.ID,
				OrganizationID: n.OrganizationID,
				UserID:         n.UserID,
				TemplateID:     n.TemplateID,
				CategoryID:     n.CategoryID,
				Title:          n.Title,
				Body:           n.Body,
				Channels:       n.Channels,
				Status:         n.Status,
				CreatedAt:      n.CreatedAt,
			}
			if n.TemplateID != nil {
				items[i].TemplateSlug = templateSlugs[*n.TemplateID]
			}
		}
		return &notificationListOutput{Body: items}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "get-notification",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/{id}",
		Summary:     "Get notification status and events",
		Tags:        []string{"Notifications"},
	}, func(ctx context.Context, input *getNotificationInput) (*notificationStatusOutput, error) {
		if err := requirePermission(ctx, auth.PermOrganizationsManage); err != nil {
			return nil, err
		}

		n, err := s.store.GetNotificationByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("notification not found")
		}

		events, err := s.store.GetNotificationEvents(ctx, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &notificationStatusOutput{}
		resp.Body.Notification = *n
		resp.Body.Events = events
		return resp, nil
	})
}
