// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type notificationIDInput struct {
	ID string `path:"id" doc:"Notification ID"`
}

type actionOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Operation result"`
	}
}

func newActionOutput() *actionOutput {
	resp := &actionOutput{}
	resp.Body.Status = "ok"
	return resp
}

type cacheDirection int

const (
	cacheIncr cacheDirection = iota
	cacheDecr
)

// updateCacheAfterAction updates the Redis unread count if the action affected it.
// Returns the current unread count (from cache INCR/DECR result or DB fallback).
func (s *Server) updateCacheAfterAction(ctx context.Context, userID string, affectsCount bool, dir cacheDirection) int {
	if !affectsCount || s.cache == nil {
		return s.getUnreadCount(ctx, userID)
	}

	var newCount int64
	var err error
	if dir == cacheIncr {
		newCount, err = s.cache.IncrUnreadCount(ctx, userID)
	} else {
		newCount, err = s.cache.DecrUnreadCount(ctx, userID)
	}

	if err != nil {
		s.logger.Error("failed to update unread count cache", "error", err)
		return s.getUnreadCount(ctx, userID)
	}

	// DecrUnreadCount returns -1 on cache miss — fall back to DB
	if newCount < 0 {
		return s.getUnreadCount(ctx, userID)
	}

	return int(newCount)
}

func (s *Server) registerActionRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "mark-read",
		Method:      http.MethodPut,
		Path:        "/v1/inbox/{id}/read",
		Summary:     "Mark a notification as read",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *notificationIDInput) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		changed, err := s.store.MarkRead(ctx, userID, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		unreadCount := s.updateCacheAfterAction(ctx, userID, changed, cacheDecr)
		s.publishInboxEvent(ctx, userID, input.ID, "read", unreadCount)
		return newActionOutput(), nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "mark-unread",
		Method:      http.MethodDelete,
		Path:        "/v1/inbox/{id}/read",
		Summary:     "Mark a notification as unread",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *notificationIDInput) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		changed, err := s.store.MarkUnread(ctx, userID, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		unreadCount := s.updateCacheAfterAction(ctx, userID, changed, cacheIncr)
		s.publishInboxEvent(ctx, userID, input.ID, "unread", unreadCount)
		return newActionOutput(), nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "archive-notification",
		Method:      http.MethodPut,
		Path:        "/v1/inbox/{id}/archive",
		Summary:     "Archive a notification",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *notificationIDInput) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		wasUnread, err := s.store.Archive(ctx, userID, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		unreadCount := s.updateCacheAfterAction(ctx, userID, wasUnread, cacheDecr)
		s.publishInboxEvent(ctx, userID, input.ID, "archive", unreadCount)
		return newActionOutput(), nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "unarchive-notification",
		Method:      http.MethodDelete,
		Path:        "/v1/inbox/{id}/archive",
		Summary:     "Unarchive a notification",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *notificationIDInput) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		nowUnread, err := s.store.Unarchive(ctx, userID, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		unreadCount := s.updateCacheAfterAction(ctx, userID, nowUnread, cacheIncr)
		s.publishInboxEvent(ctx, userID, input.ID, "unarchive", unreadCount)
		return newActionOutput(), nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "delete-notification",
		Method:      http.MethodDelete,
		Path:        "/v1/inbox/{id}",
		Summary:     "Delete a notification",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *notificationIDInput) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		wasUnread, err := s.store.SoftDelete(ctx, userID, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		unreadCount := s.updateCacheAfterAction(ctx, userID, wasUnread, cacheDecr)
		s.publishInboxEvent(ctx, userID, input.ID, "delete", unreadCount)
		return newActionOutput(), nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "mark-all-read",
		Method:      http.MethodPut,
		Path:        "/v1/inbox/read-all",
		Summary:     "Mark all notifications as read",
		Tags:        []string{"Inbox"},
	}, func(ctx context.Context, input *struct{}) (*actionOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		if err := s.store.MarkAllRead(ctx, userID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if s.cache != nil {
			if err := s.cache.SetUnreadCount(ctx, userID, 0, unreadCountTTL); err != nil {
				s.logger.Error("failed to set unread count cache", "error", err)
			}
		}
		s.publishInboxEvent(ctx, userID, "", "read-all", 0)
		return newActionOutput(), nil
	})
}
