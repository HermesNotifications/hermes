// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package inbox

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type unreadCountOutput struct {
	Body struct {
		UnreadCount int `json:"unread_count" maximum:"1000" doc:"Unread notification count. Exact below 1000; a value of 1000 means at least 1000."`
	}
}

func (s *Server) registerUnreadRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "get-unread-count",
		Method:      http.MethodGet,
		Path:        "/v1/inbox/unread-count",
		Summary:     "Get the unread notification count",
		Description: "Returns just the unread count, without fetching any notifications.\n\n" +
			"This exists for a host application that renders a bell badge on its own, with no " +
			"inbox panel mounted: such a client would otherwise have to request a page of " +
			"notifications purely to read the number off it.\n\n" +
			"Cache-backed and deliberately cheap — on a warm cache it is a single Redis read " +
			"and touches no database. It is safe to call on page load, but it is not a " +
			"substitute for the realtime channel and is not intended to be polled: it shares " +
			"the same per-user rate limit as the rest of the inbox API.",
		Tags: []string{"Inbox"},
	}, func(ctx context.Context, _ *struct{}) (*unreadCountOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		resp := &unreadCountOutput{}
		resp.Body.UnreadCount = s.unreadCount(ctx, userID)
		return resp, nil
	})
}
