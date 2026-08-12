// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package send

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
	"github.com/jackc/pgx/v5/pgxpool"
)

// SendStore defines the database operations the send service needs.
// Only API key lookup is required (for cache-miss fallback).
type SendStore interface {
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
}

// Publisher abstracts NATS message publishing for testability.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

type Server struct {
	store      SendStore
	nats       Publisher
	cache      *cache.Client
	pool       *pgxpool.Pool
	logger     *slog.Logger
	router     chi.Router
	api        huma.API
	skipAuth   bool
	hmacSecret string
	limiter    *middleware.RateLimiter
	ipLimiter  *middleware.RateLimiter
	readiness  *httputil.Readiness
}

// Default per-process rate limit. Overridable via HERMES_RATELIMIT_*; see
// ConfigureRateLimit and docs/configuration.md.
const (
	defaultRateLimitBurst     = 5000
	defaultRateLimitPerSecond = 2000

	// The pre-auth per-IP bound is a flood bound, not a quota, so it sits AT the
	// per-credential ceiling rather than below it. Anything lower would make the
	// documented per-credential limit unreachable for a legitimate caller behind
	// a single egress IP — the same reasoning the nginx annotations carry, moved
	// somewhere that does not depend on which ingress controller is installed.
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
		WithService("send").
		WithScope(middleware.ScopeCredential).
		WithLimitFunc(middleware.APIKeyLimits)
}

// ConfigureIPRateLimit installs the pre-auth per-IP bound.
//
// This runs OUTSIDE authentication, which is the whole point: the credential
// limiter cannot see an unauthenticated flood, because such a request is
// rejected by APIKeyMiddleware — after an HMAC and a Redis lookup — and never
// reaches a bucket.
func (s *Server) ConfigureIPRateLimit(enabled bool, burst, perSecond int, proxies *middleware.TrustedProxies) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultIPRateLimitBurst, defaultIPRateLimitPerSecond)
	s.ipLimiter = middleware.NewRateLimiter(proxies.ClientIP, b, p).WithScope(middleware.ScopeIP)
}

// CredentialLimiter exposes the per-credential limiter so a Reconciler can
// share its demand across replicas. The per-IP limiter is deliberately not
// exposed: it is a local flood bound whose key space an attacker chooses.
func (s *Server) CredentialLimiter() *middleware.RateLimiter { return s.limiter }

// SetSkipAuth disables API key authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store SendStore, nats Publisher, cache *cache.Client, pool *pgxpool.Pool, hmacSecret string, logger *slog.Logger) *Server {
	s := &Server{
		store:      store,
		nats:       nats,
		cache:      cache,
		pool:       pool,
		hmacSecret: hmacSecret,
		logger:     logger,
		router:     chi.NewRouter(),
	}
	s.ConfigureRateLimit(true, 0, 0)
	s.ConfigureIPRateLimit(true, 0, 0, nil)

	config := huma.DefaultConfig("Hermes Send API", "1.0.0")
	config.Info.Description = "Ultra-lightweight send endpoint for publishing notifications."
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

	s.registerSendRoutes()
}

// SetReadiness installs the readiness probe /readyz answers with, replacing the bare pool ping.
func (s *Server) SetReadiness(r *httputil.Readiness) {
	s.readiness = r
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

// requirePermission maps auth.CheckPermission onto Huma's error types.
//
// Huma handlers are func(ctx, input) and never see an http.Handler, which is why this is
// a call at the top of a handler rather than middleware — and why auth.RequirePermission,
// which returned func(http.Handler) http.Handler, could never be applied to any route.
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

func (s *Server) validateAPIKey(rawKey string) *auth.ValidatedKey {
	// s.cache is a concrete pointer, so it has to be converted deliberately: a
	// nil *cache.Client assigned straight to the interface would be non-nil and
	// every lookup would panic on a cacheless server.
	var keyCache auth.APIKeyCache
	if s.cache != nil {
		keyCache = s.cache
	}
	return auth.ResolveAPIKey(context.Background(), rawKey, s.store, keyCache, s.hmacSecret, s.logger)
}
