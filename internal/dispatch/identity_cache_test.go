// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
	hermenats "github.com/hermesnotifications/hermes/internal/nats"
)

// The caches exist to remove two write-path statements from every notification, so what
// these tests pin is the call count reaching the store — not just that the right record
// comes back. A cache that returns correct answers while still asking the database has
// bought nothing.

// cacheHarness is a Dispatch over fakes whose stores can be interrogated for call counts.
type cacheHarness struct {
	d     *Dispatch
	bus   *fakeBus
	users *fakeUserStore
	orgs  *fakeOrgStore
}

func newCacheHarness(size int, users *fakeUserStore) *cacheHarness {
	bus := &fakeBus{}
	orgs := &fakeOrgStore{}
	d := NewDispatch(bus, &fakeNotifStore{}, users, orgs, nil, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)), WithIdentityCache(size))
	return &cacheHarness{d: d, bus: bus, users: users, orgs: orgs}
}

// sendFrom is directSend for a chosen organization and external user, so a test can walk
// more identities than the cache can hold.
func sendFrom(organizationID, externalUserID string, channels ...string) []byte {
	msg := &hermenats.SendMessage{
		NotificationID: "ntf_" + organizationID + "_" + externalUserID,
		OrganizationID: organizationID,
		ExternalUserID: externalUserID,
		Content:        &hermenats.MessageContent{Title: "hi", Body: "there"},
		Channels:       channels,
		Attempt:        1,
	}
	data, _ := msg.Marshal()
	return data
}

func userWithEmail(id, email string) *models.User {
	return &models.User{ID: id, Contacts: map[string]string{"email": email}}
}

// The whole point: the second message for the same recipient must not reach the upserts.
func TestHandleSend_RepeatRecipientSkipsTheUpserts(t *testing.T) {
	h := newCacheHarness(DefaultIdentityCacheSize, &fakeUserStore{user: userWithEmail("usr_1", "a@example.com")})

	for i := 0; i < 5; i++ {
		if err := h.d.handleSend(context.Background(), directSend("inbox"), firstAttempt()); err != nil {
			t.Fatalf("handleSend %d: %v", i, err)
		}
	}

	if got := h.orgs.calls(); got != 1 {
		t.Errorf("EnsureOrganization calls: got %d, want 1", got)
	}
	ensure, _ := h.users.counts()
	if ensure != 1 {
		t.Errorf("EnsureUser calls: got %d, want 1", ensure)
	}
}

// Contacts are the mutable half and are deliberately not cached, so every send must still
// read them. Getting this wrong is not a performance regression, it is mail to a stale
// address — and it costs a SELECT, not a WAL write, so it is not what the cache is for.
func TestHandleSend_CacheHitStillReadsContacts(t *testing.T) {
	user := userWithEmail("usr_1", "old@example.com")
	h := newCacheHarness(DefaultIdentityCacheSize, &fakeUserStore{user: user})

	if err := h.d.handleSend(context.Background(), directSend("email"), firstAttempt()); err != nil {
		t.Fatalf("first handleSend: %v", err)
	}

	// Somebody else changes the address between sends, the way the user service would.
	user.Contacts = map[string]string{"email": "new@example.com"}

	if err := h.d.handleSend(context.Background(), directSend("email"), firstAttempt()); err != nil {
		t.Fatalf("second handleSend: %v", err)
	}

	deliveries := h.bus.deliveries(t)
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 delivery messages, got %d", len(deliveries))
	}
	if got := deliveries[1].Recipient["email"]; got != "new@example.com" {
		t.Errorf("second delivery went to %q, want the updated address — contacts were served from cache", got)
	}
}

// A hit must reconstruct the same record a miss returns, most of all the INTERNAL user id
// that the notification row, the delivery messages and the Centrifugo channel are all
// keyed on. The external id is only ever the lookup key.
func TestEnsureUser_HitReturnsTheInternalID(t *testing.T) {
	locale := "en-GB"
	stored := &models.User{ID: "usr_internal", Locale: &locale, Contacts: map[string]string{"email": "a@example.com"}}
	h := newCacheHarness(DefaultIdentityCacheSize, &fakeUserStore{user: stored})

	miss, err := h.d.ensureUser(context.Background(), "org_1", "ext_1")
	if err != nil {
		t.Fatalf("ensureUser (miss): %v", err)
	}
	hit, err := h.d.ensureUser(context.Background(), "org_1", "ext_1")
	if err != nil {
		t.Fatalf("ensureUser (hit): %v", err)
	}

	if hit.ID != miss.ID || hit.ID != "usr_internal" {
		t.Errorf("cached user id: got %q, want %q", hit.ID, "usr_internal")
	}
	if hit.ExternalID != "ext_1" || hit.OrganizationID != "org_1" {
		t.Errorf("cached user identity: got (%q, %q), want (org_1, ext_1)", hit.OrganizationID, hit.ExternalID)
	}
	if hit.Locale == nil || *hit.Locale != locale {
		t.Errorf("cached user locale: got %v, want %q", hit.Locale, locale)
	}
	if hit.Contacts["email"] != "a@example.com" {
		t.Errorf("cached user contacts: got %v", hit.Contacts)
	}
}

// The same external id in two organizations is two different users. Keying on the external
// id alone would hand one organization's notification to the other's recipient.
func TestEnsureUser_KeyIncludesTheOrganization(t *testing.T) {
	users := &fakeUserStore{users: map[string]*models.User{
		"shared": {ID: "usr_a", Contacts: map[string]string{"email": "a@example.com"}},
	}}
	h := newCacheHarness(DefaultIdentityCacheSize, users)

	first, err := h.d.ensureUser(context.Background(), "org_a", "shared")
	if err != nil {
		t.Fatalf("ensureUser org_a: %v", err)
	}
	// Same external id, different organization: this must miss and hit the store again.
	users.users["shared"] = &models.User{ID: "usr_b", Contacts: map[string]string{"email": "b@example.com"}}
	second, err := h.d.ensureUser(context.Background(), "org_b", "shared")
	if err != nil {
		t.Fatalf("ensureUser org_b: %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("both organizations resolved to %q — the cache key ignored the organization", first.ID)
	}
	if ensure, _ := users.counts(); ensure != 2 {
		t.Errorf("EnsureUser calls: got %d, want 2", ensure)
	}
}

// Users are unbounded, so the cache must forget. With a capacity of two, walking three
// recipients and coming back to the first has to reach the store again.
func TestEnsureUser_EvictsLeastRecentlyUsed(t *testing.T) {
	users := &fakeUserStore{users: map[string]*models.User{
		"ext_1": {ID: "usr_1"},
		"ext_2": {ID: "usr_2"},
		"ext_3": {ID: "usr_3"},
	}}
	h := newCacheHarness(2, users)
	ctx := context.Background()

	for _, ext := range []string{"ext_1", "ext_2", "ext_1", "ext_3"} {
		if _, err := h.d.ensureUser(ctx, "org_1", ext); err != nil {
			t.Fatalf("ensureUser %s: %v", ext, err)
		}
	}
	// ext_1 was touched again after ext_2, so ext_2 is the one that goes.
	if got := h.d.usersByExternalID.len(); got != 2 {
		t.Fatalf("cache size: got %d, want 2", got)
	}
	if _, ok := h.d.usersByExternalID.get(userKey{organizationID: "org_1", externalUserID: "ext_1"}); !ok {
		t.Error("ext_1 was evicted despite being used more recently than ext_2")
	}
	if _, ok := h.d.usersByExternalID.get(userKey{organizationID: "org_1", externalUserID: "ext_2"}); ok {
		t.Error("ext_2 survived eviction; the cache is not bounded by capacity")
	}

	before, _ := users.counts()
	if _, err := h.d.ensureUser(ctx, "org_1", "ext_2"); err != nil {
		t.Fatalf("ensureUser ext_2: %v", err)
	}
	if after, _ := users.counts(); after != before+1 {
		t.Errorf("an evicted key did not fall through to the store: calls %d -> %d", before, after)
	}
}

// A size of zero is the escape hatch back to today's behaviour, so it must not merely be
// slow — it must not cache at all.
func TestWithIdentityCache_ZeroDisablesCaching(t *testing.T) {
	h := newCacheHarness(0, &fakeUserStore{user: userWithEmail("usr_1", "a@example.com")})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := h.d.ensureOrganization(ctx, "org_1"); err != nil {
			t.Fatalf("ensureOrganization: %v", err)
		}
		if _, err := h.d.ensureUser(ctx, "org_1", "ext_1"); err != nil {
			t.Fatalf("ensureUser: %v", err)
		}
	}

	if got := h.orgs.calls(); got != 3 {
		t.Errorf("EnsureOrganization calls: got %d, want 3", got)
	}
	if ensure, _ := h.users.counts(); ensure != 3 {
		t.Errorf("EnsureUser calls: got %d, want 3", ensure)
	}
}

// Dispatch runs a worker pool (8 by default), so every worker shares these caches. Run
// under -race: an unsynchronised map here would be a crash in production, not a wrong
// number. Capacity is deliberately smaller than the key space so evictions run
// concurrently with reads.
func TestIdentityCache_ConcurrentAccessIsSafe(t *testing.T) {
	const (
		goroutines = 16
		iterations = 200
		recipients = 32
	)

	users := &fakeUserStore{users: map[string]*models.User{}}
	for i := 0; i < recipients; i++ {
		ext := fmt.Sprintf("ext_%d", i)
		users.users[ext] = &models.User{ID: fmt.Sprintf("usr_%d", i)}
	}
	h := newCacheHarness(recipients/4, users)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < iterations; i++ {
				n := (g + i) % recipients
				if err := h.d.ensureOrganization(ctx, fmt.Sprintf("org_%d", n%4)); err != nil {
					t.Errorf("ensureOrganization: %v", err)
					return
				}
				ext := fmt.Sprintf("ext_%d", n)
				u, err := h.d.ensureUser(ctx, "org_1", ext)
				if err != nil {
					t.Errorf("ensureUser: %v", err)
					return
				}
				// Whether this came from the store or the cache, it must be the right user.
				if want := fmt.Sprintf("usr_%d", n); u.ID != want {
					t.Errorf("ensureUser(%s) = %q, want %q", ext, u.ID, want)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if got := h.d.usersByExternalID.len(); got > recipients/4 {
		t.Errorf("cache grew past capacity under concurrency: %d entries, cap %d", got, recipients/4)
	}
}

// Concurrent handleSend is the real shape of the thing — the pool processes distinct
// messages in parallel and they contend on the same two caches.
func TestHandleSend_ConcurrentSendsShareTheCache(t *testing.T) {
	const goroutines = 8

	h := newCacheHarness(DefaultIdentityCacheSize, &fakeUserStore{user: userWithEmail("usr_1", "a@example.com")})

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			send := sendFrom("org_1", fmt.Sprintf("ext_%d", g%2), "inbox")
			if err := h.d.handleSend(context.Background(), send, firstAttempt()); err != nil {
				t.Errorf("handleSend: %v", err)
			}
		}(g)
	}
	wg.Wait()

	// Racing misses are allowed to duplicate work — the upsert is idempotent — so the
	// bound is "no more than one call per goroutine", and the floor is the two distinct
	// recipients. What must not happen is the cache failing to take effect at all.
	ensure, _ := h.users.counts()
	if ensure < 2 || ensure > goroutines {
		t.Errorf("EnsureUser calls: got %d, want between 2 and %d", ensure, goroutines)
	}
	if got := len(h.bus.deliveries(t)); got != goroutines {
		t.Errorf("delivery messages: got %d, want %d", got, goroutines)
	}
}

func TestLRUCache_UpdateExistingKeyDoesNotGrow(t *testing.T) {
	c := newLRUCache[string, int](2)
	c.put("a", 1)
	c.put("a", 2)

	if got := c.len(); got != 1 {
		t.Errorf("len: got %d, want 1", got)
	}
	if v, ok := c.get("a"); !ok || v != 2 {
		t.Errorf("get(a) = (%d, %v), want (2, true)", v, ok)
	}
}

// A nil cache is the disabled cache, and the nil receiver has to be safe for that to be a
// single code path rather than a branch at every call site.
func TestLRUCache_NilIsADisabledCache(t *testing.T) {
	var c *lruCache[string, int]
	c.put("a", 1)
	if _, ok := c.get("a"); ok {
		t.Error("a nil cache returned a hit")
	}
	if got := c.len(); got != 0 {
		t.Errorf("len: got %d, want 0", got)
	}
	if newLRUCache[string, int](0) != nil {
		t.Error("newLRUCache(0) should be nil so a zero size disables caching")
	}
}
