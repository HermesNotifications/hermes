package inbox

import (
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	if err := s.store.MarkRead(r.Context(), userID, id); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, id, "read")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	if err := s.store.MarkUnread(r.Context(), userID, id); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, id, "unread")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	if err := s.store.Archive(r.Context(), userID, id); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, id, "archive")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	if err := s.store.Unarchive(r.Context(), userID, id); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, id, "unarchive")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}
	id := r.PathValue("id")

	if err := s.store.SoftDelete(r.Context(), userID, id); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, id, "delete")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
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
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	if err := s.store.MarkAllRead(r.Context(), userID); err != nil {
		s.serverError(w, err)
		return
	}

	s.publishInboxEvent(r.Context(), userID, "", "read-all")
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
