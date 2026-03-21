package admin

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type tokenRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.UserID == "" || req.TenantID == "" {
		s.clientError(w, http.StatusBadRequest, "user_id and tenant_id are required")
		return
	}

	ctx := r.Context()

	// Validate tenant exists
	if _, err := s.store.GetTenantByID(ctx, req.TenantID); err != nil {
		s.clientError(w, http.StatusBadRequest, "unknown tenant_id")
		return
	}

	// Ensure user exists (auto-create on first token request)
	user, err := s.store.EnsureUser(ctx, req.TenantID, req.UserID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// 1h base TTL with ±10% jitter (54-66 minutes)
	baseTTL := time.Hour
	jitterRange := big.NewInt(int64(baseTTL / 5)) // 12 minutes range
	jitterBig, _ := rand.Int(rand.Reader, jitterRange)
	jitter := time.Duration(jitterBig.Int64()) - baseTTL/10
	exp := time.Now().Add(baseTTL + jitter)

	claims := &auth.HermesClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: req.TenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(s.jwtSecret)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, tokenResponse{
		Token:     tokenStr,
		ExpiresAt: exp.UTC().Format(time.RFC3339),
	})
}
