package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

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

	// Subscription Categories
	CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	DeleteCategory(ctx context.Context, id string) error

	// Subscriptions
	CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
	UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error

	// Templates
	CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error)
	GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error)
	ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error)
	UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error

	// Users
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)

	// Notifications
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)

	// API Keys
	CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error

	// JWT Signing Keys
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}

type Server struct {
	store      AdminStore
	nats       *messaging.Client
	cache      *cache.Client
	pool       *pgxpool.Pool
	logger     *slog.Logger
	router     chi.Router
	api        huma.API
	skipAuth   bool
	jwtSecret  []byte
	hmacSecret string
}

// SetSkipAuth disables API key authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store AdminStore, nats *messaging.Client, cache *cache.Client, pool *pgxpool.Pool, jwtSecret []byte, hmacSecret string, logger *slog.Logger) *Server {
	s := &Server{
		store:      store,
		nats:       nats,
		cache:      cache,
		pool:       pool,
		jwtSecret:  jwtSecret,
		hmacSecret: hmacSecret,
		logger:     logger,
		router:     chi.NewRouter(),
	}

	config := huma.DefaultConfig("Hermes Admin API", "1.0.0")
	config.Info.Description = "Server-to-server API for managing subscription categories, templates, and sending notifications."
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

	s.registerCategoryRoutes()
	s.registerSubscriptionRoutes()
	s.registerTemplateRoutes()
	s.registerSendRoutes()
	s.registerNotificationRoutes()
	s.registerAuthRoutes()
	s.registerAPIKeyRoutes()
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

func (s *Server) validateAPIKey(rawKey string) *auth.ValidatedKey {
	keyID, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		return nil
	}

	var keyHash string
	var permissions []string

	// Try cache first
	if s.cache != nil {
		cached, err := s.cache.GetAPIKey(context.Background(), keyID)
		if err != nil {
			s.logger.Error("cache get api key failed", "error", err)
		} else if cached != nil {
			var entry struct {
				KeyHash     string   `json:"key_hash"`
				Permissions []string `json:"permissions"`
			}
			if json.Unmarshal(cached, &entry) == nil {
				keyHash = entry.KeyHash
				permissions = entry.Permissions
			}
		}
	}

	// Cache miss — load from store
	if keyHash == "" {
		k, err := s.store.GetAPIKeyByID(context.Background(), keyID)
		if err != nil || k == nil {
			return nil
		}
		keyHash = k.KeyHash
		permissions = k.Permissions

		// Populate cache
		if s.cache != nil {
			entry, _ := json.Marshal(struct {
				KeyHash     string   `json:"key_hash"`
				Permissions []string `json:"permissions"`
			}{keyHash, permissions})
			if err := s.cache.SetAPIKey(context.Background(), keyID, entry, 5*time.Minute); err != nil {
				s.logger.Error("cache set api key failed", "error", err)
			}
		}
	}

	if !auth.HMACVerifyAPIKey(secret, keyHash, s.hmacSecret) {
		return nil
	}

	return &auth.ValidatedKey{ID: keyID, Permissions: permissions}
}
