// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminStore defines the database operations the admin service needs.
// The concrete *store.Store satisfies this interface.
type AdminStore interface {
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

	// Organizations
	CreateOrganization(ctx context.Context, id, name string) (*models.Organization, error)
	ListOrganizations(ctx context.Context) ([]models.Organization, error)
	CountUsersByOrganization(ctx context.Context) (map[string]int, error)

	// Users
	EnsureUser(ctx context.Context, organizationID, externalID string) (*models.User, error)
	ListUsers(ctx context.Context, organizationID string) ([]models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)

	// Notifications
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)
	ListRecentNotifications(ctx context.Context, limit int) ([]models.Notification, error)

	// API Keys
	CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error

	// JWT Signing Keys
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}

type Server struct {
	store         AdminStore
	organizations store.OrganizationRepository
	cache         *cache.Client
	pool          *pgxpool.Pool
	logger        *slog.Logger
	router        chi.Router
	api           huma.API
	skipAuth      bool
	jwtSecret     []byte
	hmacSecret    string
	limiter       *middleware.RateLimiter
}

// requirePermission maps auth.CheckPermission onto Huma's error types.
//
// Huma handlers are func(ctx, input) and never see an http.Handler, which is why this is
// a call at the top of a handler rather than middleware — and why auth.RequirePermission,
// which returned func(http.Handler) http.Handler, could never be applied to any route and
// consequently had zero call sites while being fully unit-tested.
func requirePermission(ctx context.Context, perm string) error {
	err := auth.CheckPermission(ctx, perm)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrNoAPIKey):
		return huma.Error401Unauthorized("unauthorized")
	default:
		return huma.Error403Forbidden("insufficient permissions")
	}
}

// SetSkipAuth disables API key authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

// Default per-process rate limit. Overridable via HERMES_RATELIMIT_ADMIN_*; see
// SetRateLimit and docs/configuration.md.
const (
	defaultRateLimitBurst     = 1000
	defaultRateLimitPerSecond = 500
)

// ConfigureRateLimit applies configured overrides on top of this service's
// defaults. Call before serving. A zero override keeps the default; enabled
// false turns limiting off.
func (s *Server) ConfigureRateLimit(enabled bool, burst, perSecond int) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultRateLimitBurst, defaultRateLimitPerSecond)
	s.limiter = middleware.NewRateLimiter(apiKeyLimitKey, b, p)
}

// apiKeyLimitKey buckets by validated key ID rather than by the raw
// Authorization header.
//
// The header is the wrong key twice over. APIKeyMiddleware strips the "Bearer "
// prefix before validating, so "hms_x" and "Bearer hms_x" are one credential but
// were two buckets — a free doubling of anyone's limit. And the raw header is
// the secret itself, so it had to be hashed before it could be retained. A key
// ID is neither ambiguous nor sensitive.
func apiKeyLimitKey(r *http.Request) string {
	if k := auth.GetValidatedKey(r.Context()); k != nil {
		return k.ID
	}
	return ""
}

func NewServer(store AdminStore, organizations store.OrganizationRepository, cache *cache.Client, pool *pgxpool.Pool, jwtSecret []byte, hmacSecret string, logger *slog.Logger) *Server {
	s := &Server{
		store:         store,
		organizations: organizations,
		cache:         cache,
		pool:          pool,
		jwtSecret:     jwtSecret,
		hmacSecret:    hmacSecret,
		logger:        logger,
		router:        chi.NewRouter(),
		limiter:       middleware.NewRateLimiter(apiKeyLimitKey, defaultRateLimitBurst, defaultRateLimitPerSecond),
	}

	config := huma.DefaultConfig("Hermes Admin API", "1.0.0")
	config.Info.Description = "Server-to-server API for managing subscription categories, templates, and notifications."
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
	s.registerNotificationRoutes()
	s.registerAuthRoutes()
	s.registerAPIKeyRoutes()
	s.registerOrganizationRoutes()
	s.registerUserRoutes()
}

// API returns the huma API instance for spec generation.
func (s *Server) API() huma.API {
	return s.api
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.router
	// The limiter is built once in NewServer. Constructing it here would give
	// every Handler() call a fresh, empty bucket map — which is what made the
	// limiter silently inert under test, since the suites call Handler() per
	// assertion.
	h = s.limiter.Middleware(h)
	if !s.skipAuth {
		h = auth.APIKeyMiddleware(s.validateAPIKey)(h)
	} else {
		// Skipping authentication must not also skip authorization. Injecting a fully
		// privileged key keeps handlers on the same code path as production, so
		// auth.CheckPermission can fail closed rather than carrying a nil-key exemption
		// that would be fail-open in production too. See finding 3.
		h = auth.SkipAuthMiddleware()(h)
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
