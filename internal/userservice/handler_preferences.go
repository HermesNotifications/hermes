package userservice

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
)

type setPreferenceRequest struct {
	Channels []string `json:"channels"`
}

func (s *Server) handleListPreferences(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	prefs, err := s.store.GetUserPreferences(r.Context(), userID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	var data any = prefs
	if prefs == nil {
		data = []struct{}{}
	}

	s.jsonResponse(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handleSetPreference(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	groupID := r.PathValue("group_id")
	if groupID == "" {
		s.clientError(w, http.StatusBadRequest, "group_id is required")
		return
	}

	var req setPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if len(req.Channels) == 0 {
		s.clientError(w, http.StatusBadRequest, "channels must not be empty")
		return
	}

	pref, err := s.store.SetUserPreference(r.Context(), userID, groupID, req.Channels)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, pref)
}

func (s *Server) handleDeletePreference(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	groupID := r.PathValue("group_id")
	if groupID == "" {
		s.clientError(w, http.StatusBadRequest, "group_id is required")
		return
	}

	if err := s.store.DeleteUserPreference(r.Context(), userID, groupID); err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
