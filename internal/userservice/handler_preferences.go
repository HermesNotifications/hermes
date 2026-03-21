package userservice

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
)

type setPreferenceRequest struct {
	Channels []string `json:"channels"`
}

// @Summary List notification preferences
// @Tags preferences
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/users/me/preferences [get]
// @Security BearerAuth
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

// @Summary Set notification preference for a group
// @Tags preferences
// @Accept json
// @Produce json
// @Param group_id path string true "Group ID"
// @Param body body setPreferenceRequest true "Preferred channels"
// @Success 200 {object} models.UserPreference
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/users/me/preferences/{group_id} [put]
// @Security BearerAuth
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

// @Summary Delete notification preference for a group
// @Tags preferences
// @Produce json
// @Param group_id path string true "Group ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/users/me/preferences/{group_id} [delete]
// @Security BearerAuth
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
