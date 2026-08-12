// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/models"
)

// capturingCentrifugo records the payload of each publish and can be told to fail, which is how
// the redelivery test reproduces the nack that used to double-count.
type capturingCentrifugo struct {
	server   *httptest.Server
	payloads []map[string]any
	fail     bool
}

func newCapturingCentrifugo(t *testing.T) *capturingCentrifugo {
	t.Helper()
	c := &capturingCentrifugo{}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode publish body: %v", err)
		}
		if data, ok := body["data"].(map[string]any); ok {
			c.payloads = append(c.payloads, data)
		}
		if c.fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.server.Close)
	return c
}

func testCache(t *testing.T) *cache.Client {
	t.Helper()
	url := os.Getenv("HERMES_REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	c, err := cache.Connect(url)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func uniqueSuffix() string { return time.Now().Format("150405.000000000") }

// The regression test for the overcount. A Centrifugo publish failure nacks the message, and the
// worker runs Send again for the same notification; before the guard, every retry incremented.
func TestInboxProvider_RedeliveryDoesNotDoubleCount(t *testing.T) {
	cent := newCapturingCentrifugo(t)
	redis := testCache(t)
	ctx := context.Background()

	userID := "usr_redeliver_" + uniqueSuffix()
	notifID := "ntf_redeliver_" + uniqueSuffix()

	// A live entry, as an authoritative read would have left behind.
	if err := redis.SetUnreadCount(ctx, userID, 5, "", time.Minute); err != nil {
		t.Fatalf("seed count: %v", err)
	}

	provider := NewInboxProvider(centrifugo.NewClient(cent.server.URL, "test-key"), redis, nil)
	req := DeliveryRequest{NotificationID: notifID, UserID: userID, Title: "Hello", Body: "World"}

	// First attempt: the publish fails, so the message is nacked and will be redelivered.
	cent.fail = true
	if _, err := provider.Send(ctx, req); err == nil {
		t.Fatal("expected the failed publish to return an error")
	}

	// The redelivery.
	cent.fail = false
	if _, err := provider.Send(ctx, req); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	count, _, found, err := redis.GetUnreadCountWithTTL(ctx, userID)
	if err != nil {
		t.Fatalf("GetUnreadCountWithTTL: %v", err)
	}
	if !found {
		t.Fatal("the count vanished")
	}
	if count != 6 {
		t.Fatalf("unread count = %d, want 6 -- one notification arrived, however many times it was delivered", count)
	}
}

func TestInboxProvider_AttachesUnreadCount(t *testing.T) {
	cent := newCapturingCentrifugo(t)
	redis := testCache(t)
	ctx := context.Background()

	userID := "usr_attach_" + uniqueSuffix()
	if err := redis.SetUnreadCount(ctx, userID, 3, "", time.Minute); err != nil {
		t.Fatalf("seed count: %v", err)
	}

	provider := NewInboxProvider(centrifugo.NewClient(cent.server.URL, "test-key"), redis, nil)
	req := DeliveryRequest{NotificationID: "ntf_attach_" + uniqueSuffix(), UserID: userID, Title: "Hi", Body: "There"}
	if _, err := provider.Send(ctx, req); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(cent.payloads) != 1 {
		t.Fatalf("published %d times, want 1", len(cent.payloads))
	}
	got, present := cent.payloads[0]["unread_count"]
	if !present {
		t.Fatal("notification.new carried no unread_count; the client would have to guess")
	}
	if n, ok := got.(float64); !ok || int(n) != 4 {
		t.Fatalf("unread_count = %v, want 4", got)
	}
}

// The worker has no database. On a cache miss it cannot know the count, and inventing one is how
// a badge becomes confidently wrong -- so the field must be absent, not zero.
func TestInboxProvider_OmitsUnreadCountOnCacheMiss(t *testing.T) {
	cent := newCapturingCentrifugo(t)
	redis := testCache(t)
	ctx := context.Background()

	userID := "usr_miss_" + uniqueSuffix()
	if err := redis.DeleteUnreadCount(ctx, userID); err != nil {
		t.Fatalf("clear count: %v", err)
	}

	provider := NewInboxProvider(centrifugo.NewClient(cent.server.URL, "test-key"), redis, nil)
	req := DeliveryRequest{NotificationID: "ntf_miss_" + uniqueSuffix(), UserID: userID, Title: "Hi", Body: "There"}
	if _, err := provider.Send(ctx, req); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if _, present := cent.payloads[0]["unread_count"]; present {
		t.Fatalf("notification.new carried a count the worker could not know: %v", cent.payloads[0]["unread_count"])
	}

	// And it must not have created the key either.
	if _, found, err := redis.GetUnreadCount(ctx, userID); err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	} else if found {
		t.Fatal("the arrival created a count key; only an authoritative read may do that")
	}
}

func TestInboxProvider_ClampsAtCap(t *testing.T) {
	cent := newCapturingCentrifugo(t)
	redis := testCache(t)
	ctx := context.Background()

	userID := "usr_cap_" + uniqueSuffix()
	if err := redis.SetUnreadCount(ctx, userID, models.UnreadCountCap, "", time.Minute); err != nil {
		t.Fatalf("seed count: %v", err)
	}

	provider := NewInboxProvider(centrifugo.NewClient(cent.server.URL, "test-key"), redis, nil)
	req := DeliveryRequest{NotificationID: "ntf_cap_" + uniqueSuffix(), UserID: userID, Title: "Hi", Body: "There"}
	if _, err := provider.Send(ctx, req); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := cent.payloads[0]["unread_count"]
	if n, ok := got.(float64); !ok || int(n) != models.UnreadCountCap {
		t.Fatalf("unread_count = %v, want it held at the cap %d", got, models.UnreadCountCap)
	}
}
