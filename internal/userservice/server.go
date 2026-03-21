package userservice

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
)

// UserStore defines the database operations the user service needs.
type UserStore interface {
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
	GetUserPreferences(ctx context.Context, userID string) ([]models.UserPreference, error)
	SetUserPreference(ctx context.Context, userID, groupID string, channels []string) (*models.UserPreference, error)
	DeleteUserPreference(ctx context.Context, userID, groupID string) error
	ListGroups(ctx context.Context) ([]models.NotificationGroup, error)
}

// Server is the user-facing HTTP service.
type Server struct {
	store          UserStore
	logger         *slog.Logger
	mux            *http.ServeMux
	skipAuth       bool
	jwtKeyProvider auth.JWTKeyProvider
}

// SetSkipAuth disables JWT authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store UserStore, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:          store,
		jwtKeyProvider: keyProvider,
		logger:         logger,
		mux:            http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Profile
	s.mux.HandleFunc("GET /v1/users/me", s.handleGetProfile)
	s.mux.HandleFunc("PUT /v1/users/me/contacts", s.handleUpdateContacts)

	// Preferences
	s.mux.HandleFunc("GET /v1/users/me/preferences", s.handleListPreferences)
	s.mux.HandleFunc("PUT /v1/users/me/preferences/{group_id}", s.handleSetPreference)
	s.mux.HandleFunc("DELETE /v1/users/me/preferences/{group_id}", s.handleDeletePreference)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	if !s.skipAuth {
		h = auth.JWTMiddleware(s.jwtKeyProvider)(h)
	}
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
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
