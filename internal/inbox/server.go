// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hermesnotifications/hermes/internal/auth"
	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/centrifugo"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/middleware"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/observability"
)

const (
	unreadCountTTL = 10 * time.Minute

	// unreadCountRefreshAfter is how much of the TTL may elapse before a read recomputes the
	// count ahead of expiry.
	//
	// This replaces a property the list path used to provide by accident: because every
	// GET /v1/inbox recounted from the database and overwrote the cache, drift healed on its
	// own. That healing was bounded by *traffic*, though, which is backwards -- the users who
	// most need a recount are the ones who never open the panel, and they were the ones who
	// never got one, while a user scrolling five pages paid for five counts. Bounding it by
	// time instead costs one authoritative count per user per refresh window, no matter how
	// many requests arrive.
	unreadCountRefreshAfter = 2 * time.Minute

	// unreadRefreshLeaseTTL keeps a burst of concurrent requests for one user from each firing
	// its own recount when the entry ages out. Losers serve the slightly stale cached value,
	// which is the point of refreshing ahead of expiry rather than at it.
	unreadRefreshLeaseTTL = 10 * time.Second
)

// InboxStore defines the database operations the inbox service needs.
type InboxStore interface {
	// Inbox
	ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, string, error)
	// UnreadCount saturates at models.UnreadCountCap.
	// Returns the count and the newest notification id it accounts for. Both come from one
	// snapshot; see the cache's watermark contract in internal/cache/redis.go.
	UnreadCount(ctx context.Context, userID string) (int, string, error)
	MarkRead(ctx context.Context, userID, notificationID string) (bool, error)
	MarkUnread(ctx context.Context, userID, notificationID string) (bool, error)
	Archive(ctx context.Context, userID, notificationID string) (bool, error)
	Unarchive(ctx context.Context, userID, notificationID string) (bool, error)
	SoftDelete(ctx context.Context, userID, notificationID string) (bool, error)
	MarkAllRead(ctx context.Context, userID string) error

	// Categories (for slug resolution)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
}

// UnreadCache is the slice of the Redis client this service uses for unread counts.
//
// It exists for the same reason InboxStore does: the interesting behaviour here -- refresh
// ahead of expiry, single-flighting the recount, never returning worse than the cached value
// when the store is down -- is decision logic, and pinning it to a concrete *cache.Client would
// make every one of those branches reachable only with a live Redis.
type UnreadCache interface {
	GetUnreadCountWithTTL(ctx context.Context, userID string) (int, time.Duration, bool, error)
	SetUnreadCount(ctx context.Context, userID string, count int, watermark string, ttl time.Duration) error
	DeleteUnreadCount(ctx context.Context, userID string) error
	FillUnreadCount(ctx context.Context, userID string, count int, watermark string, ttl time.Duration) (bool, error)
	TryUnreadRefreshLease(ctx context.Context, userID string, ttl time.Duration) (bool, error)
	IncrUnreadCount(ctx context.Context, userID string, maxCount int) (int64, error)
	DecrUnreadCount(ctx context.Context, userID string) (int64, error)
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
	cache          UnreadCache
	centrifugo     *centrifugo.Client
	logger         *slog.Logger
	router         chi.Router
	api            huma.API
	skipAuth       bool
	jwtKeyProvider auth.JWTKeyProvider
	limiter        *middleware.RateLimiter
	ipLimiter      *middleware.RateLimiter
	readiness      *httputil.Readiness

	// cacheDegradedLog throttles the Redis-unhealthy warnings below. They are
	// raised from the per-request read path, so an unhealthy Redis would otherwise
	// emit one record per request for the whole outage — and by design that outage
	// does not gate readiness, so it can run long. cacheResults carries the rate.
	cacheDegradedLog *observability.LogThrottle
}

// CacheDegradedLogInterval bounds how often one replica reports that the unread-count
// cache is unhealthy. See Server.cacheDegradedLog.
const CacheDegradedLogInterval = 30 * time.Second

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

	// The per-IP bound cannot sit at the per-user ceiling the way the API
	// services' does: many users legitimately share one egress IP — a corporate
	// NAT, a mobile carrier — so a bound of 20/s would throttle a whole office
	// as though it were one person. It is set well above the per-user rate and
	// is only there to stop an unauthenticated flood from reaching JWT
	// verification.
	defaultIPRateLimitBurst     = 500
	defaultIPRateLimitPerSecond = 200
)

// ConfigureRateLimit applies configured overrides on top of this service's
// defaults. Call before serving. A zero override keeps the default; enabled
// false turns limiting off.
func (s *Server) ConfigureRateLimit(enabled bool, burst, perSecond int) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultRateLimitBurst, defaultRateLimitPerSecond)
	s.limiter = middleware.NewRateLimiter(middleware.UserLimitKey, b, p).
		WithService("inbox").
		WithScope(middleware.ScopeCredential)
}

// ConfigureIPRateLimit installs the pre-auth per-IP bound, which runs before
// JWT verification so an unauthenticated flood costs no signature checks.
func (s *Server) ConfigureIPRateLimit(enabled bool, burst, perSecond int, proxies *middleware.TrustedProxies) {
	b, p := middleware.ResolveLimit(enabled, burst, perSecond, defaultIPRateLimitBurst, defaultIPRateLimitPerSecond)
	s.ipLimiter = middleware.NewRateLimiter(proxies.ClientIP, b, p).WithScope(middleware.ScopeIP)
}

// CredentialLimiter exposes the per-user limiter for reconciliation.
func (s *Server) CredentialLimiter() *middleware.RateLimiter { return s.limiter }

// SetUnreadCache substitutes the unread-count cache. Intended for use in tests only; production
// wiring goes through NewServer. Pass nil to run with no cache at all.
func (s *Server) SetUnreadCache(c UnreadCache) {
	s.cache = c
}

func NewServer(store InboxStore, cent *centrifugo.Client, cacheClient *cache.Client, keyProvider auth.JWTKeyProvider, logger *slog.Logger) *Server {
	s := &Server{
		store:            store,
		centrifugo:       cent,
		jwtKeyProvider:   keyProvider,
		logger:           logger,
		router:           chi.NewRouter(),
		cacheDegradedLog: observability.NewLogThrottle(CacheDegradedLogInterval),
	}
	s.ConfigureRateLimit(true, 0, 0)
	s.ConfigureIPRateLimit(true, 0, 0, nil)
	// Assigned only when non-nil. A nil *cache.Client stored in an interface field produces a
	// non-nil interface holding a nil pointer, so `s.cache == nil` would be false and every
	// cache call would panic on the callers that legitimately pass no cache.
	if cacheClient != nil {
		s.cache = cacheClient
	}

	config := huma.DefaultConfig("Hermes Inbox API", "1.0.0")
	config.Info.Description = "User-facing API for inbox notification management."
	config.Servers = []*huma.Server{{URL: "/"}}

	s.api = humachi.New(s.router, config)
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health checks registered directly on chi (not in OpenAPI spec).
	s.router.Get("/healthz", httputil.HealthzHandler())
	// Resolved per request rather than captured, so SetReadiness can install a probe after the
	// routes are built — the server is constructed before main knows its dependencies.
	s.router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.readiness == nil {
			httputil.ReadyzHandler()(w, r)
			return
		}
		s.readiness.Handler()(w, r)
	})

	s.registerListRoutes()
	s.registerUnreadRoutes()
	s.registerActionRoutes()
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

// unreadCount returns the user's unread count, preferring the cache.
//
// Returns -1 only when neither the cache nor the store can answer. That sentinel predates this
// change and travels all the way to the client, which is its own wart; it is preserved here
// rather than fixed, because changing it is a wire-contract change.
func (s *Server) unreadCount(ctx context.Context, userID string) int {
	if s.cache == nil {
		return s.recountUnread(ctx, userID)
	}

	count, ttl, found, err := s.cache.GetUnreadCountWithTTL(ctx, userID)
	switch {
	case err != nil:
		// Redis is unhealthy. Answer from the store, but do not write back: a failing Redis
		// under load is exactly when adding a write storm makes things worse.
		//
		// This is the only signal that Redis is in trouble. It deliberately does not gate
		// readiness — the fallback below means the pod still serves correct responses, and
		// pulling every replica out of the Service over a dependency they can work without
		// would turn a degradation into an outage. So the metric has to carry it instead.
		recordCacheResult(ctx, "unread_count", "error")
		s.cacheDegradedLog.Log(ctx, s.logger, slog.LevelWarn, "unread count cache read failed", "error", err)
		return s.recountUnread(ctx, userID)
	case !found:
		recordCacheResult(ctx, "unread_count", "miss")
		return s.fillUnreadCount(ctx, userID)
	case ttl < unreadCountTTL-unreadCountRefreshAfter:
		recordCacheResult(ctx, "unread_count", "stale")
		return s.refreshUnreadCount(ctx, userID, count)
	default:
		recordCacheResult(ctx, "unread_count", "hit")
		return count
	}
}

// fillUnreadCount seeds an absent entry from the store.
//
// There is a race here, and it is deliberately left open: this reads the store at T0, an arrival
// at T1 finds no key and so does not increment, and the T0 value lands at T2 -- one notification
// short. The window is a single indexed count, and refresh-ahead bounds the resulting error to
// one refresh interval. Closing it properly needs a separately-expiring delta key that the
// filler reconciles against, which is a lot of moving parts for a badge.
func (s *Server) fillUnreadCount(ctx context.Context, userID string) int {
	count, watermark, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get unread count", "error", err)
		return -1
	}
	if _, err := s.cache.FillUnreadCount(ctx, userID, count, watermark, unreadCountTTL); err != nil {
		// Same Redis fault as the read path, same per-request exposure, so the same
		// throttle — and Warn rather than Error, because failing to populate a cache
		// costs a store read, which is what the caller already got.
		s.cacheDegradedLog.Log(ctx, s.logger, slog.LevelWarn, "failed to cache unread count", "error", err)
	}
	return count
}

// refreshUnreadCount recomputes an aging entry under a lease, and never returns anything worse
// than the value it was given: if another request holds the lease, or the store fails, the
// cached number is still the best answer available.
func (s *Server) refreshUnreadCount(ctx context.Context, userID string, cached int) int {
	won, err := s.cache.TryUnreadRefreshLease(ctx, userID, unreadRefreshLeaseTTL)
	if err != nil {
		s.cacheDegradedLog.Log(ctx, s.logger, slog.LevelWarn, "unread count refresh lease failed", "error", err)
		return cached
	}
	if !won {
		return cached
	}

	count, watermark, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		s.logger.Error("failed to refresh unread count", "error", err)
		return cached
	}
	// Overwrite rather than SET NX: holding the lease makes this the authoritative recount, and
	// it is newer than whatever increments landed while it ran.
	if err := s.cache.SetUnreadCount(ctx, userID, count, watermark, unreadCountTTL); err != nil {
		s.logger.Error("failed to cache unread count", "error", err)
	}
	return count
}

// recountUnread reads the store without touching the cache. Used when there is no cache, or
// when the cache is the thing that just failed.
func (s *Server) recountUnread(ctx context.Context, userID string) int {
	count, _, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get unread count", "error", err)
		return -1
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
