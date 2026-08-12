// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermesnotifications/hermes/internal/auth"
	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/middleware"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store"
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
	CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string, limits models.RateLimitOverride) (*models.APIKey, error)
	UpdateAPIKeyRateLimits(ctx context.Context, id string, limits models.RateLimitOverride) (*models.APIKey, error)
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
	ipLimiter     *middleware.RateLimiter
	readiness     *httputil.Readiness
}

// SetReadiness installs the readiness probe /readyz answers with, replacing the bare pool ping.
func (s *Server) SetReadiness(r *httputil.Readiness) {
	s.readiness = r
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

// Default per-process rate limit. Overridable via HERMES_RATELIMIT_*; see
// ConfigureRateLimit and docs/configuration.md.
const (
	defaultRateLimitBurst     = 1000
	defaultRateLimitPerSecond = 500

	// A flood bound rather than a quota, so it sits at the per-credential
	// ceiling — see the equivalent note in internal/send/server.go.
	defaultIPRateLimitBurst     = defaultRateLimitBurst
	defaultIPRateLimitPerSecond = defaultRateLimitPerSecond
)

// ConfigureRateLimit applies configured overrides on top of this service's
// defaults. Call before serving. A zero override keeps the default; enabled
// false turns limiting off.
//
// The limit resolved here is the fallback. A key carrying its own
// rate_limit_per_second/_burst overrides it for that credential alone.
func (s *Server) ConfigureRateLimit(enabled bool, burst, perSecond int) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultRateLimitBurst, defaultRateLimitPerSecond)
	s.limiter = middleware.NewRateLimiter(middleware.APIKeyLimitKey, b, p).
		WithService("admin").
		WithScope(middleware.ScopeCredential).
		WithLimitFunc(middleware.APIKeyLimits)
}

// ConfigureIPRateLimit installs the pre-auth per-IP bound. See the note on the
// send service's equivalent for why this runs outside authentication.
func (s *Server) ConfigureIPRateLimit(enabled bool, burst, perSecond int, proxies *middleware.TrustedProxies) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultIPRateLimitBurst, defaultIPRateLimitPerSecond)
	s.ipLimiter = middleware.NewRateLimiter(proxies.ClientIP, b, p).WithScope(middleware.ScopeIP)
}

// CredentialLimiter exposes the per-credential limiter for reconciliation.
func (s *Server) CredentialLimiter() *middleware.RateLimiter { return s.limiter }

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
	}
	s.ConfigureRateLimit(true, 0, 0)
	s.ConfigureIPRateLimit(true, 0, 0, nil)

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
	// Resolved per request, so SetReadiness can install a probe after the routes are built.
	s.router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.readiness != nil {
			s.readiness.Handler()(w, r)
			return
		}
		if s.pool != nil {
			httputil.ReadyzHandler(s.pool.Ping)(w, r)
			return
		}
		httputil.ReadyzHandler()(w, r)
	})

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
	// Outside auth, so a flood with no valid credential is shed before it costs
	// an HMAC and a Redis lookup.
	h = s.ipLimiter.Middleware(h)
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
}

func (s *Server) validateAPIKey(rawKey string) *auth.ValidatedKey {
	// See the note in internal/send/server.go: a typed nil in an interface is
	// not nil, so the conversion has to be guarded.
	var keyCache auth.APIKeyCache
	if s.cache != nil {
		keyCache = s.cache
	}
	return auth.ResolveAPIKey(context.Background(), rawKey, s.store, keyCache, s.hmacSecret, s.logger)
}
