package admin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/httputil"
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

	// JWT Signing Keys
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}

type Server struct {
	store     AdminStore
	nats      *messaging.Client
	cache     *cache.Client
	pool      *pgxpool.Pool
	logger    *slog.Logger
	router    chi.Router
	api       huma.API
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
		router:    chi.NewRouter(),
	}

	config := huma.DefaultConfig("Hermes Admin API", "1.0.0")
	config.Info.Description = "Server-to-server API for managing notification groups, types, and sending notifications."
	config.Servers = []*huma.Server{{URL: "/"}}

	s.api = humachi.New(s.router, config)
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health checks registered directly on chi (not in OpenAPI spec)
	s.router.Get("/healthz", httputil.HealthzHandler())
	if s.pool != nil {
		s.router.Get("/readyz", httputil.ReadyzHandler(s.pool.Ping))
	} else {
		s.router.Get("/readyz", httputil.ReadyzHandler())
	}

	s.registerGroupRoutes()
	s.registerTypeRoutes()
	s.registerSendRoutes()
	s.registerNotificationRoutes()
	s.registerAuthRoutes()
}

// API returns the huma API instance for spec generation.
func (s *Server) API() huma.API {
	return s.api
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.router
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
