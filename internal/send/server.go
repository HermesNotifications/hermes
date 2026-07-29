// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package send

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
}

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
	if s.pool != nil {
		s.router.Get("/readyz", httputil.ReadyzHandler(s.pool.Ping))
	} else {
		s.router.Get("/readyz", httputil.ReadyzHandler())
	}

	s.registerSendRoutes()
}

// API returns the huma API instance for spec generation.
func (s *Server) API() huma.API {
	return s.api
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.router
	h = middleware.RateLimit(func(r *http.Request) string {
		return r.Header.Get("Authorization")
	}, 5000, 2000)(h)
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
