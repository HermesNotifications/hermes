// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

type listInboxInput struct {
	Archived bool   `query:"archived" default:"false" doc:"Filter archived notifications"`
	Cursor   string `query:"cursor" doc:"Pagination cursor"`
	Limit    int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Page size"`
}

type listInboxOutput struct {
	Body struct {
		Data        []models.Notification `json:"data" doc:"List of notifications"`
		UnreadCount int                   `json:"unread_count" maximum:"1000" doc:"Unread notification count. Exact below 1000; a value of 1000 means at least 1000."`
		Cursor      string                `json:"cursor,omitempty" doc:"Cursor for next page"`
	}
}

func (s *Server) registerListRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-inbox",
		Method:      http.MethodGet,
		Path:        "/v1/inbox",
		Summary:     "List inbox notifications",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *listInboxInput) (*listInboxOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		notifications, nextCursor, err := s.store.ListInbox(ctx, userID, input.Archived, input.Cursor, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Ensure we return [] not null in JSON
		if notifications == nil {
			notifications = []models.Notification{}
		}

		resp := &listInboxOutput{}
		resp.Body.Data = notifications
		resp.Body.UnreadCount = s.unreadCount(ctx, userID)
		resp.Body.Cursor = nextCursor
		return resp, nil
	})
}
