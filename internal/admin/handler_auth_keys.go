package admin

import (
	"encoding/json"
	"net/http"
)

type createSigningKeyRequest struct {
	Name          string `json:"name"`
	Algorithm     string `json:"algorithm"`
	Secret        string `json:"secret"`
	UserIDClaim   string `json:"user_id_claim"`
	TenantIDClaim string `json:"tenant_id_claim"`
}

func (s *Server) handleCreateSigningKey(w http.ResponseWriter, r *http.Request) {
	var req createSigningKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Name == "" || req.Secret == "" {
		s.clientError(w, http.StatusBadRequest, "name and secret are required")
		return
	}

	// Default algorithm
	if req.Algorithm == "" {
		req.Algorithm = "HS256"
	}
	// Validate algorithm
	switch req.Algorithm {
	case "HS256", "HS384", "HS512":
		// ok
	default:
		s.clientError(w, http.StatusBadRequest, "algorithm must be HS256, HS384, or HS512")
		return
	}

	// Default claim names
	if req.UserIDClaim == "" {
		req.UserIDClaim = "sub"
	}
	if req.TenantIDClaim == "" {
		req.TenantIDClaim = "tenant_id"
	}

	key, err := s.store.CreateJWTSigningKey(r.Context(), req.Name, req.Algorithm, req.Secret, req.UserIDClaim, req.TenantIDClaim)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusCreated, key)
}

func (s *Server) handleListSigningKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListJWTSigningKeys(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Ensure we return [] not null
	if keys == nil {
		s.jsonResponse(w, http.StatusOK, []struct{}{})
		return
	}

	s.jsonResponse(w, http.StatusOK, keys)
}

func (s *Server) handleDeleteSigningKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := s.store.DeleteJWTSigningKey(r.Context(), id); err != nil {
		s.clientError(w, http.StatusNotFound, "signing key not found")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}
