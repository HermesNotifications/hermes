package inbox

import (
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
)

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
