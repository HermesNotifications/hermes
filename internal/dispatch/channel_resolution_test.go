// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

// Finding 32. ResolveChannels had ZERO test coverage — its only reference outside its own
// definition was the single call site in dispatch.go. Three documents described a
// three-way precedence (explicit → user preference → category default) that the data model
// cannot express, and nothing would have failed if someone had "fixed" the code to match
// the docs instead of the reverse.
//
// These tests pin the behaviour the code actually has, so the docs and the code cannot
// drift apart again silently. The two branches with the most user-visible consequence —
// a required category ignoring an opt-out, and default_state "off" — were entirely
// unpinned before this.

// fakeChannelStore implements channelStore with just enough behaviour to resolve. Only the
// three getters ResolveChannels calls do anything; the rest satisfy the interface.
type fakeChannelStore struct {
	subscription *models.Subscription
	category     *models.SubscriptionCategory
	userSub      *models.UserSubscription
	userSubErr   error
}

func (f *fakeChannelStore) GetSubscriptionByID(context.Context, string) (*models.Subscription, error) {
	if f.subscription == nil {
		return nil, errors.New("subscription not found")
	}
	return f.subscription, nil
}

func (f *fakeChannelStore) GetCategoryByID(context.Context, string) (*models.SubscriptionCategory, error) {
	if f.category == nil {
		return nil, errors.New("category not found")
	}
	return f.category, nil
}

func (f *fakeChannelStore) GetUserSubscription(context.Context, string, string) (*models.UserSubscription, error) {
	return f.userSub, f.userSubErr
}

// Unused by ResolveChannels; present to satisfy channelStore.
func (f *fakeChannelStore) GetUserSubscriptions(context.Context, string) ([]models.UserSubscription, error) {
	return nil, nil
}
func (f *fakeChannelStore) SetUserSubscription(context.Context, string, string, bool) (*models.UserSubscription, error) {
	return nil, nil
}
func (f *fakeChannelStore) DeleteUserSubscription(context.Context, string, string) error { return nil }
func (f *fakeChannelStore) CreateSubscription(context.Context, string, string, string, int) (*models.Subscription, error) {
	return nil, nil
}
func (f *fakeChannelStore) ListSubscriptionsByCategory(context.Context, string) ([]models.Subscription, error) {
	return nil, nil
}
func (f *fakeChannelStore) UpdateSubscription(context.Context, string, string, int) (*models.Subscription, error) {
	return nil, nil
}
func (f *fakeChannelStore) DeleteSubscription(context.Context, string) error { return nil }
func (f *fakeChannelStore) CreateCategory(context.Context, string, string, []string, string, int) (*models.SubscriptionCategory, error) {
	return nil, nil
}
func (f *fakeChannelStore) GetCategoryBySlug(context.Context, string) (*models.SubscriptionCategory, error) {
	return nil, nil
}
func (f *fakeChannelStore) ListCategories(context.Context) ([]models.SubscriptionCategory, error) {
	return nil, nil
}
func (f *fakeChannelStore) UpdateCategory(context.Context, string, string, []string, string, int) (*models.SubscriptionCategory, error) {
	return nil, nil
}
func (f *fakeChannelStore) DeleteCategory(context.Context, string) error { return nil }

func subscribedTemplate() *models.NotificationTemplate {
	subID := "sub-1"
	return &models.NotificationTemplate{Slug: "welcome", SubscriptionID: &subID}
}

func storeWith(state string, defaults []string, userSub *models.UserSubscription) *fakeChannelStore {
	return &fakeChannelStore{
		subscription: &models.Subscription{ID: "sub-1", CategoryID: "cat-1"},
		category:     &models.SubscriptionCategory{ID: "cat-1", DefaultState: state, DefaultChannels: defaults},
		userSub:      userSub,
	}
}

func optedIn(in bool) *models.UserSubscription {
	return &models.UserSubscription{UserID: "usr-1", SubscriptionID: "sub-1", OptedIn: in}
}

func TestResolveChannels_StandaloneTemplate(t *testing.T) {
	standalone := &models.NotificationTemplate{Slug: "standalone", DefaultChannels: []string{"email"}}
	// nil store: a standalone template must not consult subscriptions or categories at all.
	cr := NewChannelResolver(&fakeChannelStore{}, nil)
	ctx := context.Background()

	t.Run("explicit channels win over the template default", func(t *testing.T) {
		got, err := cr.ResolveChannels(ctx, []string{"sms"}, "usr-1", standalone)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "sms" {
			t.Errorf("got %v, want [sms]", got)
		}
	})

	t.Run("falls back to the template default", func(t *testing.T) {
		got, err := cr.ResolveChannels(ctx, nil, "usr-1", standalone)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "email" {
			t.Errorf("got %v, want [email]", got)
		}
	})

	t.Run("errors when neither is available", func(t *testing.T) {
		bare := &models.NotificationTemplate{Slug: "bare"}
		if _, err := cr.ResolveChannels(ctx, nil, "usr-1", bare); err == nil {
			t.Error("expected an error when a standalone template has no channels to use")
		}
	})
}

// The compliance-adjacent branch: a required category delivers even to a user who has
// explicitly opted out. That is deliberate, and it is exactly the behaviour someone
// "simplifying" this function would remove without noticing.
func TestResolveChannels_RequiredCategoryIgnoresOptOut(t *testing.T) {
	cr := NewChannelResolver(storeWith("required", []string{"email"}, optedIn(false)), nil)

	got, err := cr.ResolveChannels(context.Background(), nil, "usr-1", subscribedTemplate())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "email" {
		t.Errorf("got %v, want [email] — a required category must ignore an opt-out", got)
	}
}

// A user preference is a boolean gate, not a channel selection: user_subscriptions has no
// channel column. Opting in yields the set already resolved, unchanged.
func TestResolveChannels_UserPreferenceIsAGateNotASelection(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		userSub  *models.UserSubscription
		explicit []string
		want     []string
	}{
		{
			name:    "opted out yields nothing",
			state:   "on",
			userSub: optedIn(false),
			want:    nil,
		},
		{
			name:    "opted in yields the category default unchanged",
			state:   "on",
			userSub: optedIn(true),
			want:    []string{"email", "inbox"},
		},
		{
			name:     "opted in yields explicit channels, which REPLACE the default wholesale",
			state:    "on",
			userSub:  optedIn(true),
			explicit: []string{"sms"},
			want:     []string{"sms"},
		},
		{
			name:    "no stored preference falls through to default_state on",
			state:   "on",
			userSub: nil,
			want:    []string{"email", "inbox"},
		},
		{
			name:    "no stored preference with default_state off yields nothing",
			state:   "off",
			userSub: nil,
			want:    nil,
		},
		{
			name:    "an explicit opt-in overrides default_state off",
			state:   "off",
			userSub: optedIn(true),
			want:    []string{"email", "inbox"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr := NewChannelResolver(storeWith(tc.state, []string{"email", "inbox"}, tc.userSub), nil)

			got, err := cr.ResolveChannels(context.Background(), tc.explicit, "usr-1", subscribedTemplate())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
					break
				}
			}
		})
	}
}
