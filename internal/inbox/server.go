package inbox

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/messaging"
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

	// Groups (for slug resolution)
	GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error)
}

// Server is the inbox HTTP service.
type Server struct {
	store          InboxStore
	cache          *cache.Client
	centrifugo     *centrifugo.Client
	nats           *messaging.Client
	logger         *slog.Logger
	mux            *http.ServeMux
	skipAuth       bool
	jwtKeyProvider auth.JWTKeyProvider
}

// SetSkipAuth disables JWT authentication. Intended for use in tests only.
func (s *Server) SetSkipAuth(skip bool) {
	s.skipAuth = skip
}

func NewServer(store InboxStore, cent *centrifugo.Client, nats *messaging.Client, cacheClient *cache.Client, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:          store,
		cache:          cacheClient,
		centrifugo:     cent,
		nats:           nats,
		jwtKeyProvider: keyProvider,
		logger:         logger,
		mux:            http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	s.mux.HandleFunc("GET /readyz", httputil.ReadyzHandler())

	// Inbox
	s.mux.HandleFunc("GET /v1/inbox", s.handleListInbox)
	s.mux.HandleFunc("PUT /v1/inbox/read-all", s.handleMarkAllRead)
	s.mux.HandleFunc("PUT /v1/inbox/{id}/read", s.handleMarkRead)
	s.mux.HandleFunc("DELETE /v1/inbox/{id}/read", s.handleMarkUnread)
	s.mux.HandleFunc("PUT /v1/inbox/{id}/archive", s.handleArchive)
	s.mux.HandleFunc("DELETE /v1/inbox/{id}/archive", s.handleUnarchive)
	s.mux.HandleFunc("DELETE /v1/inbox/{id}", s.handleSoftDelete)
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

// publishInboxEvent publishes a control event to the user's Centrifugo channel.
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
		s.logger.Error("failed to publish centrifugo event", "error", err, "channel", channel, "action", action)
	}
}
