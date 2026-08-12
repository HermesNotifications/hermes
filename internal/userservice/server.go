// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package userservice

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
)

// UserStore defines the database operations the user service needs.
type UserStore interface {
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	SetUserContact(ctx context.Context, userID, addressKey, address string) error
	GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error)
	SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error)
	DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
}

// Server is the user-facing HTTP service.
type Server struct {
	store          UserStore
	logger         *slog.Logger
	router         chi.Router
	api            huma.API
	skipAuth       bool
	jwtKeyProvider auth.JWTKeyProvider
	limiter        *middleware.RateLimiter
	ipLimiter      *middleware.RateLimiter
	readiness      *httputil.Readiness
}

// SetReadiness installs the readiness probe /readyz answers with. Without one the endpoint
// reports ready unconditionally, which is what it did before dependencies were checked at all.
func (s *Server) SetReadiness(r *httputil.Readiness) {
	s.readiness = r
}

// SetSkipAuth disables JWT authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

// Default per-process rate limit. Overridable via HERMES_RATELIMIT_*; see
// ConfigureRateLimit and docs/configuration.md.
const (
	defaultRateLimitBurst     = 50
	defaultRateLimitPerSecond = 20

	// Set well above the per-user rate: many users share one egress IP behind a
	// corporate NAT or a mobile carrier, so a bound at the per-user ceiling
	// would throttle a whole office as though it were one person. See the
	// equivalent note in internal/inbox/server.go.
	defaultIPRateLimitBurst     = 500
	defaultIPRateLimitPerSecond = 200
)

// ConfigureRateLimit applies configured overrides on top of this service's
// defaults. Call before serving. A zero override keeps the default; enabled
// false turns limiting off.
func (s *Server) ConfigureRateLimit(enabled bool, burst, perSecond int) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultRateLimitBurst, defaultRateLimitPerSecond)
	s.limiter = middleware.NewRateLimiter(middleware.UserLimitKey, b, p).WithScope(middleware.ScopeCredential)
}

// ConfigureIPRateLimit installs the pre-auth per-IP bound, which runs before
// JWT verification so an unauthenticated flood costs no signature checks.
func (s *Server) ConfigureIPRateLimit(enabled bool, burst, perSecond int, proxies *middleware.TrustedProxies) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultIPRateLimitBurst, defaultIPRateLimitPerSecond)
	s.ipLimiter = middleware.NewRateLimiter(proxies.ClientIP, b, p).WithScope(middleware.ScopeIP)
}

// CredentialLimiter exposes the per-user limiter for reconciliation.
func (s *Server) CredentialLimiter() *middleware.RateLimiter { return s.limiter }

func NewServer(store UserStore, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:          store,
		jwtKeyProvider: keyProvider,
		logger:         logger,
		router:         chi.NewRouter(),
	}
	s.ConfigureRateLimit(true, 0, 0)
	s.ConfigureIPRateLimit(true, 0, 0, nil)

	config := huma.DefaultConfig("Hermes User API", "1.0.0")
	config.Info.Description = "User-facing API for profile management and notification preferences."
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
		if s.readiness == nil {
			httputil.ReadyzHandler()(w, r)
			return
		}
		s.readiness.Handler()(w, r)
	})

	s.registerProfileRoutes()
	s.registerPreferenceRoutes()
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
		h = auth.JWTMiddleware(s.jwtKeyProvider)(h)
	}
	// Outside auth, so an unauthenticated flood is shed before JWT verification.
	h = s.ipLimiter.Middleware(h)
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
}
