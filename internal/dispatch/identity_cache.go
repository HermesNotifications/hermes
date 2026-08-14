// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/hermesnotifications/hermes/internal/models"
)

// DefaultIdentityCacheSize is the per-cache capacity NewDispatch uses when the caller
// passes no WithIdentityCache option. It is a memory bound, not a tuning knob that has
// been swept: an entry is on the order of a couple of hundred bytes, so ten thousand of
// them is single-digit megabytes per replica, and ten thousand distinct recipients is
// more than the working set of any install this has been measured against. Installs with
// a larger active user population should raise it — the cost of being too small is a
// miss, which is exactly the behaviour that exists today.
const DefaultIdentityCacheSize = 10_000

// handleSend makes three store calls per message, and two of them ask for something that
// is already true: EnsureOrganization and EnsureUser are `INSERT ... ON CONFLICT DO
// NOTHING` upserts, so after the first message for a given organization or user they
// write no row — but they are still write-path statements that take a connection and
// take part in a commit.
//
// That matters because dispatch is not CPU-bound. A June 2026 sweep measured ~1,242
// notifications/s on one replica and ~2,006/s on four (four times the replicas for 1.6x
// the throughput) while Postgres never exceeded 2.5 of 24 cores; `pg_test_fsync` on the
// same volume reports 1,933 fdatasync ops/s at 517us, which agrees with the measured
// ceiling within 4%. The wall is WAL fsync, so the useful thing to remove is statements
// that reach the WAL, and these two are removable because their answer never changes.
//
// The cache is deliberately in-process and per-replica. A shared Redis cache would trade
// a Postgres round trip for a Redis one, and the point here is to make no round trip at
// all; each replica warming its own copy costs one upsert per organization or user per
// replica, which is nothing against the traffic that follows.
//
// Two misses racing for the same key is harmless: both call the upsert, which is
// idempotent, and both store the same value. Nothing here uses singleflight because the
// only thing it would save is a duplicate write on the very first message of a key, on a
// pool that defaults to 8 workers.
//
// The one way a cached answer can become wrong is a row being deleted underneath a warm
// replica: the notification insert would then fail its foreign key until the entry is
// evicted or the process restarts. Nothing in the running system does that — no service
// deletes a user or an organization — and the only code that does is cmd/loadseed's
// cleanup, run against an idle install between load tests. Restart dispatch (or send
// enough distinct traffic to evict) if you delete recipients under a live one.

// userKey identifies a user the way the send path does — by the caller's own identifier
// scoped to their organization — which is NOT how the rest of the system identifies one.
// The value behind this key is the INTERNAL user id, and the two must never be conflated:
// notifications, deliveries and Centrifugo channels are all keyed on the internal id,
// while the external id is only ever an input to the lookup.
type userKey struct {
	organizationID string
	externalUserID string
}

// cachedUser is the part of a user row that cannot change once the row exists, which is
// the only part that is safe to remember.
//
// Contacts are conspicuously absent. A user's email or phone is mutable — the user
// service writes it through SetUserContact/UpdateUserContacts — and dispatch reads it on
// every send to decide which channels have somewhere to deliver to. Caching it would mean
// mailing a stale address, or dropping a channel whose contact point was added a minute
// ago, until an eviction happened to fix it, and nothing invalidates one replica's map
// from another process. So a cache hit still reads contacts. That read is a SELECT, not a
// write: it never touches the WAL, and so it is not the round trip this cache exists to
// remove.
//
// The remaining columns (locale, created_at) are never updated by any store method; they
// are carried so a cache hit reconstructs the same record a miss would return, rather
// than a half-populated one that invites a future reader to trust a zero value.
type cachedUser struct {
	id        string
	locale    *string
	createdAt time.Time
}

func newCachedUser(u *models.User) cachedUser {
	return cachedUser{id: u.ID, locale: u.Locale, createdAt: u.CreatedAt}
}

func (c cachedUser) user(key userKey) *models.User {
	return &models.User{
		ID:             c.id,
		OrganizationID: key.organizationID,
		ExternalID:     key.externalUserID,
		Locale:         c.locale,
		CreatedAt:      c.createdAt,
	}
}

// ensureOrganization guarantees the organization row exists, skipping the store once this
// replica has seen the id before.
//
// Only existence is remembered — not the record. handleSend discards it, and the fields
// the store would return (name, default locale, settings) are all editable through the
// admin API, so keeping them would be keeping something that can go stale for no
// benefit.
//
// In production d.organizations is already cached.OrganizationRepository, so the call this
// saves is usually a Redis GET rather than a Postgres upsert. That is still worth saving:
// it is a network round trip on a worker that is holding a message, and unlike the Redis
// lookup it cannot fail or time out.
func (d *Dispatch) ensureOrganization(ctx context.Context, organizationID string) error {
	if _, ok := d.organizationsSeen.get(organizationID); ok {
		return nil
	}
	if _, err := d.organizations.EnsureOrganization(ctx, organizationID); err != nil {
		return err
	}
	d.organizationsSeen.put(organizationID, struct{}{})
	return nil
}

// ensureUser returns the user for (organizationID, externalUserID), creating the row on
// first sight. A hit skips the upsert and re-reads only the mutable half — see cachedUser
// for why contacts are not remembered.
func (d *Dispatch) ensureUser(ctx context.Context, organizationID, externalUserID string) (*models.User, error) {
	key := userKey{organizationID: organizationID, externalUserID: externalUserID}

	if cached, ok := d.usersByExternalID.get(key); ok {
		contacts, err := d.users.GetUserContacts(ctx, cached.id)
		if err != nil {
			return nil, err
		}
		u := cached.user(key)
		u.Contacts = contacts
		return u, nil
	}

	u, err := d.users.EnsureUser(ctx, organizationID, externalUserID)
	if err != nil {
		return nil, err
	}
	d.usersByExternalID.put(key, newCachedUser(u))
	return u, nil
}

// lruCache is a fixed-capacity, least-recently-used map.
//
// Bounded because users are unbounded: a plain map keyed by user would grow for the life
// of the process and never shrink, which is a leak with a slow fuse. LRU rather than TTL
// because what is cached does not expire — a user row that exists keeps existing — so
// there is nothing for a clock to fix, and the only question is which entries are worth
// the memory. "The ones used most recently" is the right answer for a workload that
// arrives in bursts per organization.
//
// One mutex rather than sharding or a lock-free map: the critical section is a map lookup
// and two pointer swaps, while the miss it protects against is a database round trip
// measured in hundreds of microseconds. At the default pool of 8 workers there is no
// contention worth engineering for. A pool orders of magnitude larger would be the reason
// to revisit this, not a suspicion that a mutex is slow.
//
// A nil *lruCache is a valid disabled cache: every miss, every put a no-op. That is what
// makes a size of zero mean "no caching" without a second code path, and what lets tests
// construct a Dispatch by struct literal and get today's uncached behaviour.
type lruCache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	// order is most-recently-used first; elements hold *lruEntry[K, V].
	order *list.List
	index map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// newLRUCache returns nil for a non-positive capacity, which disables the cache.
func newLRUCache[K comparable, V any](capacity int) *lruCache[K, V] {
	if capacity <= 0 {
		return nil
	}
	return &lruCache[K, V]{
		capacity: capacity,
		order:    list.New(),
		index:    make(map[K]*list.Element, capacity),
	}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[key]
	if !ok {
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry[K, V]).value, true
}

func (c *lruCache[K, V]) put(key K, value V) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		el.Value.(*lruEntry[K, V]).value = value
		c.order.MoveToFront(el)
		return
	}

	c.index[key] = c.order.PushFront(&lruEntry[K, V]{key: key, value: value})
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.index, oldest.Value.(*lruEntry[K, V]).key)
	}
}

func (c *lruCache[K, V]) len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.index)
}
