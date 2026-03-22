package admin

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/httputil"
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

// @Summary List notification types
// @Tags types
// @Produce json
// @Success 200 {array} models.NotificationType
// @Failure 500 {object} map[string]string
// @Router /v1/types [get]
// @Security ApiKeyAuth
func (s *Server) handleListTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.store.ListTypes(r.Context())
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}
	httputil.JSON(w, http.StatusOK, types)
}

// @Summary Create a notification type
// @Tags types
// @Accept json
// @Produce json
// @Param body body createTypeRequest true "Type to create"
// @Success 201 {object} models.NotificationType
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/types [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateType(w http.ResponseWriter, r *http.Request) {
	var req createTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ClientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Slug == "" || req.Name == "" || req.GroupID == "" {
		httputil.ClientError(w, http.StatusBadRequest, "slug, name, and group_id are required")
		return
	}

	nt, err := s.store.CreateType(r.Context(), &models.NotificationType{
		GroupID: req.GroupID, Slug: req.Slug, Name: req.Name,
		EmailSubject: req.EmailSubject, EmailBody: req.EmailBody,
		SMSBody: req.SMSBody, InboxTitle: req.InboxTitle, InboxBody: req.InboxBody,
	})
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, nt)
}

// @Summary Update a notification type
// @Tags types
// @Accept json
// @Produce json
// @Param id path string true "Type ID"
// @Param body body updateTypeRequest true "Fields to update"
// @Success 200 {object} models.NotificationType
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/types/{id} [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateType(w http.ResponseWriter, r *http.Request) {
	typeID := r.PathValue("id")
	var req updateTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ClientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	existing, err := s.store.GetTypeByID(r.Context(), typeID)
	if err != nil {
		httputil.ClientError(w, http.StatusNotFound, "type not found")
		return
	}

	updated, err := s.store.UpdateType(r.Context(), &models.NotificationType{
		ID: typeID, Name: req.Name,
		EmailSubject: req.EmailSubject, EmailBody: req.EmailBody,
		SMSBody: req.SMSBody, InboxTitle: req.InboxTitle, InboxBody: req.InboxBody,
	})
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	if s.cache != nil {
		s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)
	}

	httputil.JSON(w, http.StatusOK, updated)
}

// @Summary Delete a notification type
// @Tags types
// @Param id path string true "Type ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/types/{id} [delete]
// @Security ApiKeyAuth
func (s *Server) handleDeleteType(w http.ResponseWriter, r *http.Request) {
	typeID := r.PathValue("id")

	existing, err := s.store.GetTypeByID(r.Context(), typeID)
	if err != nil {
		httputil.ClientError(w, http.StatusNotFound, "type not found")
		return
	}

	if err := s.store.DeleteType(r.Context(), typeID); err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	if s.cache != nil {
		s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)
	}
	w.WriteHeader(http.StatusNoContent)
}
