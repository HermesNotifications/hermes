package inbox

import (
	"context"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/httputil"
)

// @Summary Mark a notification as read
// @Tags inbox
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/{id}/read [put]
// @Security BearerAuth
func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	changed, err := s.store.MarkRead(r.Context(), userID, id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	unreadCount := s.updateCacheAfterAction(r.Context(), userID, changed, cacheDecr)
	s.publishInboxEvent(r.Context(), userID, id, "read", unreadCount)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// @Summary Mark a notification as unread
// @Tags inbox
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/{id}/read [delete]
// @Security BearerAuth
func (s *Server) handleMarkUnread(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	changed, err := s.store.MarkUnread(r.Context(), userID, id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	unreadCount := s.updateCacheAfterAction(r.Context(), userID, changed, cacheIncr)
	s.publishInboxEvent(r.Context(), userID, id, "unread", unreadCount)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// @Summary Archive a notification
// @Tags inbox
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/{id}/archive [put]
// @Security BearerAuth
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	wasUnread, err := s.store.Archive(r.Context(), userID, id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	unreadCount := s.updateCacheAfterAction(r.Context(), userID, wasUnread, cacheDecr)
	s.publishInboxEvent(r.Context(), userID, id, "archive", unreadCount)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// @Summary Unarchive a notification
// @Tags inbox
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/{id}/archive [delete]
// @Security BearerAuth
func (s *Server) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	nowUnread, err := s.store.Unarchive(r.Context(), userID, id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	unreadCount := s.updateCacheAfterAction(r.Context(), userID, nowUnread, cacheIncr)
	s.publishInboxEvent(r.Context(), userID, id, "unarchive", unreadCount)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// @Summary Delete a notification
// @Tags inbox
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/{id} [delete]
// @Security BearerAuth
func (s *Server) handleSoftDelete(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	wasUnread, err := s.store.SoftDelete(r.Context(), userID, id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	unreadCount := s.updateCacheAfterAction(r.Context(), userID, wasUnread, cacheDecr)
	s.publishInboxEvent(r.Context(), userID, id, "delete", unreadCount)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// @Summary Mark all notifications as read
// @Tags inbox
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox/read-all [put]
// @Security BearerAuth
func (s *Server) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	if err := s.store.MarkAllRead(r.Context(), userID); err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	if s.cache != nil {
		if err := s.cache.SetUnreadCount(r.Context(), userID, 0, unreadCountTTL); err != nil {
			s.logger.Error("failed to set unread count cache", "error", err)
		}
	}
	s.publishInboxEvent(r.Context(), userID, "", "read-all", 0)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
