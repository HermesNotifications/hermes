package userservice

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
)

type updateContactsRequest struct {
	Email *string `json:"email"`
	Phone *string `json:"phone"`
}

// @Summary Get current user profile
// @Tags users
// @Produce json
// @Success 200 {object} models.User
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/users/me [get]
// @Security BearerAuth
func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, user)
}

// @Summary Update user contact information
// @Tags users
// @Accept json
// @Produce json
// @Param body body updateContactsRequest true "Contact fields to update"
// @Success 200 {object} models.User
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/users/me/contacts [put]
// @Security BearerAuth
func (s *Server) handleUpdateContacts(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	var req updateContactsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Email == nil && req.Phone == nil {
		s.clientError(w, http.StatusBadRequest, "at least one of email or phone must be provided")
		return
	}

	user, err := s.store.UpdateUserContacts(r.Context(), userID, req.Email, req.Phone)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, user)
}
