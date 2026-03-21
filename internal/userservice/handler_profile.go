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
