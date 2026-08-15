// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/httputil"
	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/middleware")

// rateLimitCounter is deliberately labelled by decision and scope alone. The
// caller key is the one label an operator would reach for and the one that must
// never be a label — it is unbounded by construction. Scope is safe because it
// takes one of a handful of compile-time values. Which service reported the
// metric comes from the OTel resource's service.name, so it needs no label.
var rateLimitCounter, _ = meter.Int64Counter(
	"hermes.http.rate_limit_decisions",
	metric.WithDescription("HTTP requests seen by the rate limiter, by whether they were allowed, limited, or admitted unlimited because the entry map was full."),
	metric.WithUnit("1"),
)

// sharedFailures counts requests decided locally because the cluster-wide
// limiter could not answer. A non-zero rate means the advertised limit is no
// longer cluster-wide, which is the alertable condition — requests are still
// being served, so nothing else surfaces it.
var sharedFailures, _ = meter.Int64Counter(
	"hermes.http.rate_limit_backend_failures",
	metric.WithDescription("Admission checks that fell back to the local bucket because the shared limiter was unavailable."),
	metric.WithUnit("1"),
)

var (
	decisionAllowed = attribute.String("decision", "allowed")
	decisionLimited = attribute.String("decision", "limited")
	// decisionOverflow marks a request admitted without a bucket because the entry
	// map was full. Only the credential scope can produce it, and it is the signal
	// that maxEntries is below the size of the population being limited -- which is
	// otherwise invisible, since each admitted request looks like any other.
	decisionOverflow = attribute.String("decision", "overflow_admitted")
)

const (
	// How often the entry map is swept, and how long an entry must sit idle
	// before the sweep drops it.
	sweepInterval = 5 * time.Minute
	entryTTL      = 30 * time.Minute

	// How often a full-map reclaim may run once the entry cap is reached.
	forcedSweepInterval = time.Second

	// DefaultMaxEntries bounds the entry map.
	//
	// It matters most for the pre-auth per-IP limiter, whose key space is chosen
	// by the caller rather than by us: a scan across a /16 would otherwise
	// allocate 65k buckets long before the five-minute sweep runs. Credential
	// scopes are naturally bounded by how many keys exist, so the cap is close to
	// free there.
	DefaultMaxEntries = 50_000

	// overflowKey is the single bucket every caller shares once the cap is hit.
	// It cannot collide with a real key: an IP renders as an address, an API key
	// ID is base62, and a user ID is prefixed.
	overflowKey = "\x00overflow"
)

// ScopeIP and friends name what a limiter buckets by. The value appears in the
// metric and in the shared limiter's Redis keys, so it must be stable.
const (
	ScopeIP         = "ip"
	ScopeCredential = "credential"
)

// LimitFunc returns the burst and sustained rate that apply to one request,
// allowing a limit to vary per credential. Returning (0, 0) — or being nil —
// means "use the limiter's configured default", which is the same sentinel
// ResolveLimit uses.
type LimitFunc func(*http.Request) (burst, perSecond int)

// SharedLimiter is a cluster-wide admission check. *cache.Client satisfies it.
//
// An error means the decision could not be made, not that the request should be
// refused: RateLimiter falls back to its local bucket. Implementations must
// therefore bound their own latency rather than blocking, because the caller
// always has a usable answer available locally.
type SharedLimiter interface {
	AllowRequest(ctx context.Context, key string, burst, perSecond int) (cache.RateLimitDecision, error)
}

// rateLimitEntry is one caller's bucket plus the last time it was touched, so
// idle entries can be reclaimed.
type rateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // Unix nanoseconds

	// limit and burst are what this caller is entitled to. They are fixed for the
	// entry's lifetime and are what gets advertised and, when a shared limiter is
	// installed, what gets asked for cluster-wide.
	limit rate.Limit
	burst int
}

// RateLimiter is a per-caller token bucket keyed by whatever keyFunc extracts
// from the request.
//
// Without a shared limiter, enforcement is per process: with more than one
// replica the effective ceiling is the limit multiplied by the replica count.
//
// WithShared makes the limit cluster-wide. The shared backend is then
// authoritative and the local bucket becomes the fallback used only when that
// backend cannot answer — so a Redis outage degrades to per-replica enforcement
// rather than either failing requests or removing the limit altogether.
//
// Construct one per server and reuse it. Building a fresh limiter per request
// chain gives every caller a full bucket and enforces nothing.
type RateLimiter struct {
	keyFunc   func(*http.Request) string
	limitFunc LimitFunc
	scope     string
	service   string

	shared SharedLimiter
	logger *slog.Logger

	limit rate.Limit
	burst int

	entries         sync.Map // string -> *rateLimitEntry
	entryCount      atomic.Int64
	maxEntries      int
	lastSweep       atomic.Int64
	lastForcedSweep atomic.Int64

	// Precomputed so the hot path adds a counter without building an attribute
	// set per request.
	attrAllowed  metric.MeasurementOption
	attrLimited  metric.MeasurementOption
	attrOverflow metric.MeasurementOption
	attrScope    metric.MeasurementOption

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
		keyFunc:    keyFunc,
		limit:      limit,
		burst:      burst,
		maxEntries: DefaultMaxEntries,
		now:        time.Now,
	}
	rl.WithScope(ScopeCredential)
	rl.lastSweep.Store(time.Now().UnixNano())
	return rl
}

// WithShared makes this limiter's limit cluster-wide.
//
// Pass only limiters whose key space is ours rather than the caller's — in
// practice the credential scopes. The per-IP limiter is deliberately left local:
// it is a flood bound rather than a quota, and sending an attacker-chosen key
// space to Redis would turn an address scan into Redis load, which is the
// opposite of what a flood bound is for.
func (rl *RateLimiter) WithShared(s SharedLimiter, logger *slog.Logger) *RateLimiter {
	rl.shared = s
	rl.logger = logger
	return rl
}

// WithService namespaces this limiter's shared keys.
//
// Without it every service writes `rl:credential:<id>` into one Redis, so a single API key
// calling both Send and Admin shares one bucket — and the two pass different rates for that
// bucket, so a send burst pushes out the shared allowance and Admin returns 429s for
// something the caller never did. Inbox and User collide the same way on the JWT user ID,
// silently halving each user's documented per-service quota.
//
// It does not appear in the metric: which service reported comes from the OTel resource's
// service.name, so a label would duplicate it.
func (rl *RateLimiter) WithService(name string) *RateLimiter {
	rl.service = name
	return rl
}

// WithScope names what this limiter buckets by, for metrics and Redis keys.
func (rl *RateLimiter) WithScope(scope string) *RateLimiter {
	rl.scope = scope
	scopeAttr := attribute.String("scope", scope)
	rl.attrAllowed = metric.WithAttributes(decisionAllowed, scopeAttr)
	rl.attrLimited = metric.WithAttributes(decisionLimited, scopeAttr)
	rl.attrOverflow = metric.WithAttributes(decisionOverflow, scopeAttr)
	rl.attrScope = metric.WithAttributes(scopeAttr)
	return rl
}

// WithLimitFunc installs a per-request limit lookup, so one limiter can enforce
// a different rate for each credential.
//
// The limit is read once, when a caller's bucket is first created, and is then
// pinned for that bucket's lifetime. Re-reading it per request would let a
// changed limit silently discard accumulated tokens; instead a change takes
// effect when the entry ages out (entryTTL) or the key is invalidated.
func (rl *RateLimiter) WithLimitFunc(f LimitFunc) *RateLimiter {
	rl.limitFunc = f
	return rl
}

// WithMaxEntries bounds the entry map. Zero or less restores the default.
func (rl *RateLimiter) WithMaxEntries(n int) *RateLimiter {
	if n <= 0 {
		n = DefaultMaxEntries
	}
	rl.maxEntries = n
	return rl
}

// Scope reports what this limiter buckets by.
func (rl *RateLimiter) Scope() string { return rl.scope }

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

		key := rl.keyFunc(r)
		entry := rl.entryFor(key, r)

		// nil means the map is full and this scope fails open rather than sharing a
		// bucket with strangers -- see atCapacity. Admit, and count it, so a cap set
		// below the size of the population shows up as a rising
		// decision=overflow_admitted rather than as nothing at all.
		if entry == nil {
			rateLimitCounter.Add(r.Context(), 1, rl.attrOverflow)
			next.ServeHTTP(w, r)
			return
		}

		entry.lastSeen.Store(now.UnixNano())

		// An entry whose entitlement is Inf is not limited at all. Skipping the
		// check keeps the disabled path free of both bucket arithmetic and a
		// pointless Redis round trip.
		if entry.limit == rate.Inf {
			rateLimitCounter.Add(r.Context(), 1, rl.attrAllowed)
			next.ServeHTTP(w, r)
			return
		}

		if rl.shared != nil && rl.allowShared(w, r, key, entry, next) {
			return
		}

		rl.allowLocal(w, r, now, entry, next)
	})
}

// allowShared decides the request against the cluster-wide limiter.
//
// It reports whether it handled the request. False means the backend could not
// answer and the caller should fall back to the local bucket — never that the
// request was refused, which is signalled by returning true having already
// written the 429.
//
// The local bucket is deliberately NOT consumed on this path. Two authorities
// charging for the same request would double-count, and the fallback bucket
// starting full is the behaviour we want anyway: a backend outage should look
// like the per-replica enforcement that was in place before, not like a cliff.
func (rl *RateLimiter) allowShared(
	w http.ResponseWriter,
	r *http.Request,
	key string,
	entry *rateLimitEntry,
	next http.Handler,
) bool {
	dec, err := rl.shared.AllowRequest(r.Context(), rl.sharedKey(key), entry.burst, int(entry.limit))
	if err != nil {
		// Fail over, loudly but not fatally. The caller still gets a decision;
		// it is just made from this replica's own state.
		//
		// Counted with the scope alone: a fallback is not a "limited" decision, and most
		// of these requests are then admitted by the local bucket. Labelling them
		// decision=limited made any dashboard splitting on it read the opposite.
		sharedFailures.Add(r.Context(), 1, rl.attrScope)
		if rl.logger != nil {
			rl.logger.Warn("shared rate limiter unavailable; falling back to the local bucket",
				"scope", rl.scope, "error", err)
		}
		return false
	}

	if !dec.Allowed {
		rateLimitCounter.Add(r.Context(), 1, rl.attrLimited)
		rl.writeLimited(w, entry, true, dec.RetryAfter)
		return true
	}

	rateLimitCounter.Add(r.Context(), 1, rl.attrAllowed)
	rl.setLimitHeaders(w, entry, float64(dec.Remaining))
	next.ServeHTTP(w, r)
	return true
}

// sharedKey namespaces a caller's key by service and scope. See WithService.
// cache.AllowRequest adds its own "rl:" prefix, so this does not.
func (rl *RateLimiter) sharedKey(key string) string {
	if rl.service == "" {
		return rl.scope + ":" + key
	}
	return rl.service + ":" + rl.scope + ":" + key
}

// allowLocal decides the request against this process's own bucket.
func (rl *RateLimiter) allowLocal(
	w http.ResponseWriter,
	r *http.Request,
	now time.Time,
	entry *rateLimitEntry,
	next http.Handler,
) {
	res := entry.limiter.ReserveN(now, 1)
	delay := res.DelayFrom(now)
	if !res.OK() || delay > 0 {
		// Hand the token back. A reservation we do not honour still spends
		// future capacity, so without this a client retrying in a tight loop
		// pushes its own next success further and further out and never
		// recovers.
		res.CancelAt(now)
		rateLimitCounter.Add(r.Context(), 1, rl.attrLimited)
		rl.writeLimited(w, entry, res.OK(), delay)
		return
	}

	rateLimitCounter.Add(r.Context(), 1, rl.attrAllowed)
	rl.setLimitHeaders(w, entry, entry.limiter.TokensAt(now))
	next.ServeHTTP(w, r)
}

// entryFor returns the bucket for key, creating it if needed.
func (rl *RateLimiter) entryFor(key string, r *http.Request) *rateLimitEntry {
	// Load before LoadOrStore: the store path allocates a limiter, and on a
	// warm bucket — which is nearly every request — that allocation is garbage.
	if v, ok := rl.entries.Load(key); ok {
		return v.(*rateLimitEntry)
	}

	// At the cap, try to reclaim before refusing. A sweep here is the difference
	// between shedding genuinely idle buckets and punishing live callers.
	//
	// Rate-limited, because the sweep is a full scan of the map and the situation that
	// triggers it is an address scan: without this, every request bearing an unseen key
	// costs a 50,000-entry traversal, and concurrent requests each run their own. The
	// mitigation for a cardinality attack would have been its best amplifier. Between
	// sweeps we go straight to the shared overflow bucket, which is the intended
	// behaviour under exactly this load.
	if rl.entryCount.Load() >= int64(rl.maxEntries) {
		now := rl.now()
		last := rl.lastForcedSweep.Load()
		if now.UnixNano()-last < int64(forcedSweepInterval) ||
			!rl.lastForcedSweep.CompareAndSwap(last, now.UnixNano()) {
			return rl.atCapacity()
		}
		rl.forceSweep(now)
		if rl.entryCount.Load() >= int64(rl.maxEntries) {
			return rl.atCapacity()
		}
	}

	limit, burst := rl.limit, rl.burst

	// A per-credential limit must not resurrect a limiter the operator turned off.
	// HERMES_RATELIMIT_ENABLED=false resolves to (0, 0) — Inf rate, zero burst — and
	// applying a key's own rate on top of that produced a finite rate with a burst of
	// zero, which x/time/rate refuses every single request against. Disabling rate
	// limiting entirely locked out exactly the keys that had a limit configured.
	if rl.limitFunc != nil && r != nil && rl.limit != rate.Inf {
		if b, ps := rl.limitFunc(r); ps > 0 || b > 0 {
			if ps > 0 {
				limit = rate.Limit(ps)
			}
			if b > 0 {
				burst = b
			}
		}
	}

	// A finite rate with no burst admits nothing, so a key that sets only its sustained
	// rate takes the rate as its burst rather than becoming unusable.
	if limit != rate.Inf && burst < 1 {
		burst = max(int(limit), 1)
	}

	fresh := &rateLimitEntry{
		limiter: rate.NewLimiter(limit, burst),
		limit:   limit,
		burst:   burst,
	}
	actual, loaded := rl.entries.LoadOrStore(key, fresh)
	if !loaded {
		rl.entryCount.Add(1)
	}
	return actual.(*rateLimitEntry)
}

// atCapacity decides what a key gets when the map is full, and the answer depends
// on who chose the key.
//
// The per-IP limiter runs before authentication over a key space the caller picks,
// so a /16 scan can mint 65,000 keys on demand. Collapsing those into one bucket is
// the whole mitigation: the flood throttles itself, and callers caught alongside it
// are still served at the configured rate for as long as it lasts.
//
// The credential scopes are the opposite. Their key space is ours -- an API key id
// or a user id that exists because we issued it -- so a key beyond the cap is not
// evidence of an attack, it is evidence that the cap is smaller than the user base.
// Sharing a bucket there inverts the limiter's purpose: it exists to stop one caller
// affecting others, and instead makes every caller past the cap limit each other.
//
// Measured, not hypothesised. A 100,000-connection run polling the inbox at 100 rps
// crossed 50,000 distinct users after N*ln2 requests -- 693 seconds -- and from that
// moment roughly half of all requests fell into one 20/s bucket: 6,705 429s in four
// minutes, to users three orders of magnitude below their own limit. See
// docs/loadtest/realtime-scale-2026-08-14.md and ADR 0024.
//
// So credential scopes fail open, returning nil for "admit without a bucket". The
// alternative -- refusing -- would turn a capacity shortfall into an outage for
// everyone the cap could not fit, which is strictly worse than not limiting them.
// A nil return is counted as decision=overflow_admitted so it is visible rather than
// silent.
func (rl *RateLimiter) atCapacity() *rateLimitEntry {
	if rl.scope == ScopeCredential {
		return nil
	}
	return rl.overflowEntry()
}

// overflowEntry returns the shared bucket used once the map is full. See atCapacity
// for why only the caller-chosen key spaces reach it.
func (rl *RateLimiter) overflowEntry() *rateLimitEntry {
	if v, ok := rl.entries.Load(overflowKey); ok {
		return v.(*rateLimitEntry)
	}
	fresh := &rateLimitEntry{
		limiter: rate.NewLimiter(rl.limit, rl.burst),
		limit:   rl.limit,
		burst:   rl.burst,
	}
	actual, loaded := rl.entries.LoadOrStore(overflowKey, fresh)
	if !loaded {
		rl.entryCount.Add(1)
	}
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
	rl.evictIdleSince(now.Add(-entryTTL))
}

// forceSweep reclaims regardless of when the last sweep ran. Used when the map
// is at its cap, where waiting for the interval would mean shedding live traffic
// into the overflow bucket for up to five minutes.
func (rl *RateLimiter) forceSweep(now time.Time) {
	rl.lastSweep.Store(now.UnixNano())
	rl.evictIdleSince(now.Add(-entryTTL))
}

func (rl *RateLimiter) evictIdleSince(cutoff time.Time) {
	c := cutoff.UnixNano()
	rl.entries.Range(func(key, value any) bool {
		if key == overflowKey {
			return true // the shared bucket is not owned by any caller
		}
		if value.(*rateLimitEntry).lastSeen.Load() < c {
			if _, loaded := rl.entries.LoadAndDelete(key); loaded {
				rl.entryCount.Add(-1)
			}
		}
		return true
	})
}

// advertises reports whether this limiter should describe itself in RateLimit-*
// headers.
//
// Only the credential scope does. The client contract defines RateLimit-Limit as
// "requests allowed for this credential" (docs/integration-guide.md), and the
// per-IP bound is a different thing measured against a different key — a caller
// sharing an office NAT would read the flood bound as its own quota and size
// against a number that has nothing to do with its credential. A 429 from the
// per-IP limiter still carries Retry-After, which is the part a client can act
// on.
func (rl *RateLimiter) advertises() bool {
	return rl.scope != ScopeIP
}

func (rl *RateLimiter) setLimitHeaders(w http.ResponseWriter, entry *rateLimitEntry, tokens float64) {
	if entry.limit == rate.Inf || !rl.advertises() {
		// Limiting is off, or this scope does not describe the caller's quota.
		// Advertising a limit would be a lie, and int(rate.Inf) is not a number
		// anyone wants in a header.
		return
	}
	h := w.Header()
	// The advertised limit is the caller's cluster-wide entitlement, not this
	// replica's current share. A client that saw its share would watch the number
	// move for reasons that have nothing to do with its own behaviour.
	h.Set("RateLimit-Limit", strconv.Itoa(int(entry.limit)))
	h.Set("RateLimit-Remaining", strconv.Itoa(max(int(tokens), 0)))
}

func (rl *RateLimiter) writeLimited(w http.ResponseWriter, entry *rateLimitEntry, ok bool, delay time.Duration) {
	// Retry-After is whole seconds and must be at least 1, so a sub-second wait
	// still rounds up rather than telling the client to retry immediately.
	retryAfter := 1
	if ok && delay > 0 {
		retryAfter = max(int(math.Ceil(delay.Seconds())), 1)
	}

	h := w.Header()
	if rl.advertises() {
		h.Set("RateLimit-Limit", strconv.Itoa(int(entry.limit)))
		h.Set("RateLimit-Remaining", "0")
		h.Set("RateLimit-Reset", strconv.Itoa(retryAfter))
	}
	h.Set("Retry-After", strconv.Itoa(retryAfter))
	httputil.ClientError(w, http.StatusTooManyRequests, "rate limit exceeded")
}
