package admin

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/models"
)

type createTypeRequest struct {
	GroupID      string  `json:"group_id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	EmailSubject *string `json:"email_subject"`
	EmailBody    *string `json:"email_body"`
	SMSBody      *string `json:"sms_body"`
	InboxTitle   *string `json:"inbox_title"`
	InboxBody    *string `json:"inbox_body"`
}

type updateTypeRequest struct {
	Name         string  `json:"name"`
	EmailSubject *string `json:"email_subject"`
	EmailBody    *string `json:"email_body"`
	SMSBody      *string `json:"sms_body"`
	InboxTitle   *string `json:"inbox_title"`
	InboxBody    *string `json:"inbox_body"`
}

func (s *Server) handleListTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.store.ListTypes(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, types)
}

func (s *Server) handleCreateType(w http.ResponseWriter, r *http.Request) {
	var req createTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Slug == "" || req.Name == "" || req.GroupID == "" {
		s.clientError(w, http.StatusBadRequest, "slug, name, and group_id are required")
		return
	}

	nt, err := s.store.CreateType(r.Context(), &models.NotificationType{
		GroupID: req.GroupID, Slug: req.Slug, Name: req.Name,
		EmailSubject: req.EmailSubject, EmailBody: req.EmailBody,
		SMSBody: req.SMSBody, InboxTitle: req.InboxTitle, InboxBody: req.InboxBody,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusCreated, nt)
}

func (s *Server) handleUpdateType(w http.ResponseWriter, r *http.Request) {
	typeID := r.PathValue("id")
	var req updateTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	existing, err := s.store.GetTypeByID(r.Context(), typeID)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "type not found")
		return
	}

	updated, err := s.store.UpdateType(r.Context(), &models.NotificationType{
		ID: typeID, Name: req.Name,
		EmailSubject: req.EmailSubject, EmailBody: req.EmailBody,
		SMSBody: req.SMSBody, InboxTitle: req.InboxTitle, InboxBody: req.InboxBody,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}

	if s.cache != nil {
		s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)
	}

	s.jsonResponse(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteType(w http.ResponseWriter, r *http.Request) {
	typeID := r.PathValue("id")

	existing, err := s.store.GetTypeByID(r.Context(), typeID)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "type not found")
		return
	}

	if err := s.store.DeleteType(r.Context(), typeID); err != nil {
		s.serverError(w, err)
		return
	}

	if s.cache != nil {
		s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)
	}
	w.WriteHeader(http.StatusNoContent)
}
