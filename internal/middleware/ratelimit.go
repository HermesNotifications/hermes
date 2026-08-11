// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
)

var meter = observability.Meter("github.com/hermes-notifications/hermes/internal/middleware")

// rateLimitCounter is deliberately labelled by decision alone. The caller key is
// the one label an operator would reach for and the one that must never be a
// label — it is unbounded by construction. Which service reported the metric
// comes from the OTel resource's service.name, so it needs no label either.
var rateLimitCounter, _ = meter.Int64Counter(
	"hermes.http.rate_limit_decisions",
	metric.WithDescription("HTTP requests seen by the rate limiter, by whether they were allowed or limited."),
	metric.WithUnit("1"),
)

var (
	decisionAllowed = metric.WithAttributes(attribute.String("decision", "allowed"))
	decisionLimited = metric.WithAttributes(attribute.String("decision", "limited"))
)

const (
	// How often the entry map is swept, and how long an entry must sit idle
	// before the sweep drops it.
	sweepInterval = 5 * time.Minute
	entryTTL      = 30 * time.Minute
)

// rateLimitEntry is one caller's bucket plus the last time it was touched, so
// idle entries can be reclaimed.
type rateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // Unix nanoseconds
}

// RateLimiter is a per-caller token bucket keyed by whatever keyFunc extracts
// from the request.
//
// The limit is enforced PER PROCESS. With more than one replica the effective
// cluster limit is this limit multiplied by the replica count, and under an HPA
// it moves with the autoscaler. That is a deliberate, documented property — see
// docs/configuration.md — not an oversight.
//
// Construct one per server and reuse it. Building a fresh limiter per request
// chain gives every caller a full bucket and enforces nothing.
type RateLimiter struct {
	keyFunc func(*http.Request) string
	limit   rate.Limit
	burst   int

	entries   sync.Map // string -> *rateLimitEntry
	lastSweep atomic.Int64

	// now is overridable so tests can advance time without sleeping.
	now func() time.Time
}

// NewRateLimiter returns a limiter allowing burst requests immediately and
// perSecond requests per second sustained.
// A perSecond of zero or less disables limiting entirely, which is how
// HERMES_RATELIMIT_ENABLED=false is expressed without a second code path.
func NewRateLimiter(keyFunc func(*http.Request) string, burst, perSecond int) *RateLimiter {
	limit := rate.Limit(perSecond)
	if perSecond <= 0 {
		limit = rate.Inf
	}
	rl := &RateLimiter{
		keyFunc: keyFunc,
		limit:   limit,
		burst:   burst,
		now:     time.Now,
	}
	rl.lastSweep.Store(time.Now().UnixNano())
	return rl
}

// ResolveLimit applies configured overrides on top of a service's defaults.
//
// A zero or negative override keeps the default, so a deployment can tune burst
// without also having to restate the sustained rate. When enabled is false the
// result is (0, 0), which NewRateLimiter treats as unlimited.
func ResolveLimit(enabled bool, burst, perSecond, defBurst, defPerSecond int) (int, int) {
	if !enabled {
		return 0, 0
	}
	if burst <= 0 {
		burst = defBurst
	}
	if perSecond <= 0 {
		perSecond = defPerSecond
	}
	return burst, perSecond
}

// Middleware applies the limiter to next.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health probes are never limited. They bypass auth (see
		// auth.APIKeyMiddleware), so every probe carries an empty key and they
		// would all contend for one shared bucket — turning a burst of probes
		// into a readiness failure that has nothing to do with the service.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}

		now := rl.now()
		rl.sweep(now)

		entry := rl.entryFor(rl.keyFunc(r))
		entry.lastSeen.Store(now.UnixNano())

		res := entry.limiter.ReserveN(now, 1)
		delay := res.DelayFrom(now)
		if !res.OK() || delay > 0 {
			// Hand the token back. A reservation we do not honour still spends
			// future capacity, so without this a client retrying in a tight loop
			// pushes its own next success further and further out and never
			// recovers.
			res.CancelAt(now)
			rateLimitCounter.Add(r.Context(), 1, decisionLimited)
			rl.writeLimited(w, res.OK(), delay)
			return
		}

		rateLimitCounter.Add(r.Context(), 1, decisionAllowed)
		rl.setLimitHeaders(w, entry.limiter.TokensAt(now))
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) entryFor(key string) *rateLimitEntry {
	// Load before LoadOrStore: the store path allocates a limiter, and on a
	// warm bucket — which is nearly every request — that allocation is garbage.
	if v, ok := rl.entries.Load(key); ok {
		return v.(*rateLimitEntry)
	}
	fresh := &rateLimitEntry{limiter: rate.NewLimiter(rl.limit, rl.burst)}
	actual, _ := rl.entries.LoadOrStore(key, fresh)
	return actual.(*rateLimitEntry)
}

// sweep drops entries idle for longer than entryTTL.
//
// It runs inline on whichever request first crosses the interval rather than on
// a background ticker. A ticker needs an owner to stop it; the previous
// implementation started one per middleware construction and stopped none of
// them, which leaked a goroutine per call in the test suites.
func (rl *RateLimiter) sweep(now time.Time) {
	last := rl.lastSweep.Load()
	if now.UnixNano()-last < int64(sweepInterval) {
		return
	}
	if !rl.lastSweep.CompareAndSwap(last, now.UnixNano()) {
		return // another request won the race and is sweeping
	}

	cutoff := now.Add(-entryTTL).UnixNano()
	rl.entries.Range(func(key, value any) bool {
		if value.(*rateLimitEntry).lastSeen.Load() < cutoff {
			rl.entries.Delete(key)
		}
		return true
	})
}

func (rl *RateLimiter) setLimitHeaders(w http.ResponseWriter, tokens float64) {
	if rl.limit == rate.Inf {
		// Limiting is off; advertising a limit would be a lie, and int(rate.Inf)
		// is not a number anyone wants in a header.
		return
	}
	h := w.Header()
	h.Set("RateLimit-Limit", strconv.Itoa(int(rl.limit)))
	h.Set("RateLimit-Remaining", strconv.Itoa(max(int(tokens), 0)))
}

func (rl *RateLimiter) writeLimited(w http.ResponseWriter, ok bool, delay time.Duration) {
	// Retry-After is whole seconds and must be at least 1, so a sub-second wait
	// still rounds up rather than telling the client to retry immediately.
	retryAfter := 1
	if ok && delay > 0 {
		retryAfter = max(int(math.Ceil(delay.Seconds())), 1)
	}

	h := w.Header()
	h.Set("RateLimit-Limit", strconv.Itoa(int(rl.limit)))
	h.Set("RateLimit-Remaining", "0")
	h.Set("RateLimit-Reset", strconv.Itoa(retryAfter))
	h.Set("Retry-After", strconv.Itoa(retryAfter))
	httputil.ClientError(w, http.StatusTooManyRequests, "rate limit exceeded")
}
