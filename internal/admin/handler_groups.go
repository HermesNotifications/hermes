package admin

import (
	"encoding/json"
	"net/http"
)

type createGroupRequest struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
}

type updateGroupRequest struct {
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
}

// @Summary List notification groups
// @Tags groups
// @Produce json
// @Success 200 {array} models.NotificationGroup
// @Failure 500 {object} map[string]string
// @Router /v1/groups [get]
// @Security ApiKeyAuth
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, groups)
}

// @Summary Create a notification group
// @Tags groups
// @Accept json
// @Produce json
// @Param body body createGroupRequest true "Group to create"
// @Success 201 {object} models.NotificationGroup
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/groups [post]
// @Security ApiKeyAuth
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Slug == "" || req.Name == "" {
		s.clientError(w, http.StatusBadRequest, "slug and name are required")
		return
	}

	g, err := s.store.CreateGroup(r.Context(), req.Slug, req.Name, req.DefaultChannels)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusCreated, g)
}

// @Summary Update a notification group
// @Tags groups
// @Accept json
// @Produce json
// @Param id path string true "Group ID"
// @Param body body updateGroupRequest true "Fields to update"
// @Success 200 {object} models.NotificationGroup
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/groups/{id} [put]
// @Security ApiKeyAuth
func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	g, err := s.store.UpdateGroup(r.Context(), id, req.Name, req.DefaultChannels)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, g)
}

// jsonResponse writes a JSON-encoded response with the given status code.
func (s *Server) jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// clientError writes a JSON error response with the given status and message.
func (s *Server) clientError(w http.ResponseWriter, status int, message string) {
	s.jsonResponse(w, status, map[string]string{"error": message})
}

// serverError logs the error and writes a 500 JSON response.
func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.logger.Error("internal error", "error", err)
	s.clientError(w, http.StatusInternalServerError, "internal server error")
}
