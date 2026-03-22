package inbox

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/models"
)

// InboxStore defines the database operations the inbox service needs.
type InboxStore interface {
	// Inbox
	ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error)
	MarkRead(ctx context.Context, userID, notificationID string) error
	MarkUnread(ctx context.Context, userID, notificationID string) error
	Archive(ctx context.Context, userID, notificationID string) error
	Unarchive(ctx context.Context, userID, notificationID string) error
	SoftDelete(ctx context.Context, userID, notificationID string) error
	MarkAllRead(ctx context.Context, userID string) error

	// Groups (for slug resolution)
	GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error)
}

// Server is the inbox HTTP service.
type Server struct {
	store          InboxStore
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

func NewServer(store InboxStore, cent *centrifugo.Client, nats *messaging.Client, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:          store,
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

// publishInboxEvent publishes a control event to the user's Centrifugo channel.
func (s *Server) publishInboxEvent(ctx context.Context, userID, notificationID, action string) {
	if s.centrifugo == nil {
		return
	}
	event := map[string]string{
		"type":            "inbox.updated",
		"notification_id": notificationID,
		"action":          action,
	}
	channel := "user#" + userID
	if err := s.centrifugo.Publish(ctx, channel, event); err != nil {
		s.logger.Error("failed to publish centrifugo event", "error", err, "channel", channel, "action", action)
	}
}
