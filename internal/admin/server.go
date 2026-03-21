package admin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminStore defines the database operations the admin service needs.
// The concrete *store.Store satisfies this interface.
type AdminStore interface {
	// Tenants
	GetTenantByID(ctx context.Context, id string) (*models.Tenant, error)

	// Groups
	CreateGroup(ctx context.Context, slug, name string, defaultChannels []string) (*models.NotificationGroup, error)
	GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error)
	GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error)
	ListGroups(ctx context.Context) ([]models.NotificationGroup, error)
	UpdateGroup(ctx context.Context, id, name string, defaultChannels []string) (*models.NotificationGroup, error)

	// Types
	CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error)
	GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error)
	ListTypes(ctx context.Context) ([]models.NotificationType, error)
	UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	DeleteType(ctx context.Context, id string) error

	// Users
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)

	// Notifications
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)

	// API Keys
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
}

type Server struct {
	store     AdminStore
	nats      *messaging.Client
	cache     *cache.Client
	pool      *pgxpool.Pool
	logger    *slog.Logger
	mux       *http.ServeMux
	skipAuth  bool
	jwtSecret []byte
}

// SetSkipAuth disables API key authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store AdminStore, nats *messaging.Client, cache *cache.Client, pool *pgxpool.Pool, jwtSecret []byte, logger *slog.Logger) *Server {
	s := &Server{
		store:     store,
		nats:      nats,
		cache:     cache,
		pool:      pool,
		jwtSecret: jwtSecret,
		logger:    logger,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Groups
	s.mux.HandleFunc("GET /v1/groups", s.handleListGroups)
	s.mux.HandleFunc("POST /v1/groups", s.handleCreateGroup)
	s.mux.HandleFunc("PUT /v1/groups/{id}", s.handleUpdateGroup)

	// Types
	s.mux.HandleFunc("GET /v1/types", s.handleListTypes)
	s.mux.HandleFunc("POST /v1/types", s.handleCreateType)
	s.mux.HandleFunc("PUT /v1/types/{id}", s.handleUpdateType)
	s.mux.HandleFunc("DELETE /v1/types/{id}", s.handleDeleteType)

	// Send
	s.mux.HandleFunc("POST /v1/send", s.handleSend)

	// Notifications
	s.mux.HandleFunc("GET /v1/notifications/{id}", s.handleGetNotification)

	// Auth token exchange
	s.mux.HandleFunc("POST /v1/auth/token", s.handleAuthToken)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = middleware.RateLimit(func(r *http.Request) string {
		return r.Header.Get("Authorization")
	}, 1000, 500)(h)
	if !s.skipAuth {
		h = auth.APIKeyMiddleware(s.validateAPIKey)(h)
	}
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
}

func (s *Server) validateAPIKey(rawKey string) bool {
	keys, err := s.store.ListAPIKeys(context.Background())
	if err != nil {
		s.logger.Error("failed to load API keys", "error", err)
		return false
	}
	for _, k := range keys {
		if auth.VerifyAPIKey(rawKey, k.KeyHash) {
			return true
		}
	}
	return false
}
