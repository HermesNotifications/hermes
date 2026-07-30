// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package inbox

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
)

const unreadCountTTL = 10 * time.Minute

// InboxStore defines the database operations the inbox service needs.
type InboxStore interface {
	// Inbox
	ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error)
	UnreadCount(ctx context.Context, userID string) (int, error)
	MarkRead(ctx context.Context, userID, notificationID string) (bool, error)
	MarkUnread(ctx context.Context, userID, notificationID string) (bool, error)
	Archive(ctx context.Context, userID, notificationID string) (bool, error)
	Unarchive(ctx context.Context, userID, notificationID string) (bool, error)
	SoftDelete(ctx context.Context, userID, notificationID string) (bool, error)
	MarkAllRead(ctx context.Context, userID string) error

	// Categories (for slug resolution)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
}

// Server is the inbox HTTP service.
//
// It holds no NATS client on purpose. It never published or subscribed to anything — the
// field was dead — and under ADR 0005 phase 3 that matters: a connection needs an NKey user
// and a set of subject permissions, so a client kept "just in case" is a credential granted
// for nothing. Real-time push to this service's users goes out through the inbox worker over
// Centrifugo's HTTP API, not the bus.
type Server struct {
	store          InboxStore
	cache          *cache.Client
	centrifugo     *centrifugo.Client
	logger         *slog.Logger
	router         chi.Router
	api            huma.API
	skipAuth       bool
	jwtKeyProvider auth.JWTKeyProvider
}

// SetSkipAuth disables JWT authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store InboxStore, cent *centrifugo.Client, cacheClient *cache.Client, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:          store,
		cache:          cacheClient,
		centrifugo:     cent,
		jwtKeyProvider: keyProvider,
		logger:         logger,
		router:         chi.NewRouter(),
	}

	config := huma.DefaultConfig("Hermes Inbox API", "1.0.0")
	config.Info.Description = "User-facing API for inbox notification management."
	config.Servers = []*huma.Server{{URL: "/"}}

	s.api = humachi.New(s.router, config)
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health checks registered directly on chi (not in OpenAPI spec)
	s.router.Get("/healthz", httputil.HealthzHandler())
	s.router.Get("/readyz", httputil.ReadyzHandler())

	s.registerListRoutes()
	s.registerActionRoutes()
}

// API returns the huma API instance for spec generation.
func (s *Server) API() huma.API {
	return s.api
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.router
	h = middleware.RateLimit(func(r *http.Request) string {
		return auth.UserIDFromContext(r.Context())
	}, 50, 20)(h)
	if !s.skipAuth {
		h = auth.JWTMiddleware(s.jwtKeyProvider)(h)
	}
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
}

// getUnreadCount reads the unread count from cache, falling back to DB on miss.
func (s *Server) getUnreadCount(ctx context.Context, userID string) int {
	if s.cache != nil {
		count, found, err := s.cache.GetUnreadCount(ctx, userID)
		if err == nil && found {
			return count
		}
	}
	// Cache miss or error — fall back to DB
	count, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get unread count", "error", err)
		return -1
	}
	if s.cache != nil {
		if err := s.cache.SetUnreadCount(ctx, userID, count, unreadCountTTL); err != nil {
			s.logger.Error("failed to cache unread count", "error", err)
		}
	}
	return count
}

// publishInboxEvent publishes a control event to the user's real-time channel.
func (s *Server) publishInboxEvent(ctx context.Context, userID, notificationID, action string, unreadCount int) {
	if s.centrifugo == nil {
		return
	}
	event := map[string]any{
		"type":            "inbox.updated",
		"notification_id": notificationID,
		"action":          action,
		"unread_count":    unreadCount,
		"timestamp":       time.Now().UnixMilli(),
	}
	channel := "user#" + userID
	if err := s.centrifugo.Publish(ctx, channel, event); err != nil {
		s.logger.Error("failed to publish real-time event", "error", err, "channel", channel, "action", action)
	}
}
