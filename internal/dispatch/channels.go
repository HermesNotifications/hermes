// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/models"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/provider"
	"github.com/hermes-notifications/hermes/internal/store"
)

// channelStore composes the repository interfaces needed for channel resolution.
type channelStore interface {
	store.UserSubscriptionRepository
	store.SubscriptionRepository
	store.SubscriptionCategoryRepository
}

type ChannelResolver struct {
	store channelStore
	cache *cache.Client
}

func NewChannelResolver(store channelStore, cache *cache.Client) *ChannelResolver {
	return &ChannelResolver{store: store, cache: cache}
}

func (cr *ChannelResolver) resolveSubscription(ctx context.Context, id string) (*models.Subscription, error) {
	if cr.cache != nil {
		data, err := cr.cache.GetSubscription(ctx, id)
		if err == nil && data != nil {
			var sub models.Subscription
			if err := json.Unmarshal(data, &sub); err == nil {
				return &sub, nil
			}
		}
	}
	sub, err := cr.store.GetSubscriptionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cr.cache != nil {
		if data, err := json.Marshal(sub); err == nil {
			_ = cr.cache.SetSubscription(ctx, id, data, 5*time.Minute)
		}
	}
	return sub, nil
}

func (cr *ChannelResolver) resolveCategory(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	if cr.cache != nil {
		data, err := cr.cache.GetCategory(ctx, id)
		if err == nil && data != nil {
			var cat models.SubscriptionCategory
			if err := json.Unmarshal(data, &cat); err == nil {
				return &cat, nil
			}
		}
	}
	cat, err := cr.store.GetCategoryByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cr.cache != nil {
		if data, err := json.Marshal(cat); err == nil {
			_ = cr.cache.SetCategory(ctx, id, data, 5*time.Minute)
		}
	}
	return cat, nil
}

// ResolveChannels determines target channels for a template-based send.
// For templates with a subscription: required check -> user pref -> category default.
// For standalone templates: explicit channels -> template default_channels.
func (cr *ChannelResolver) ResolveChannels(ctx context.Context, explicitChannels []string, userID string, template *models.NotificationTemplate) ([]string, error) {
	// Standalone template (no subscription)
	if template.SubscriptionID == nil {
		if len(explicitChannels) > 0 {
			return explicitChannels, nil
		}
		if len(template.DefaultChannels) > 0 {
			return template.DefaultChannels, nil
		}
		return nil, fmt.Errorf("standalone template %s has no default channels and no explicit channels provided", template.Slug)
	}

	// Template with subscription — resolve category
	sub, err := cr.resolveSubscription(ctx, *template.SubscriptionID)
	if err != nil {
		return nil, err
	}
	cat, err := cr.resolveCategory(ctx, sub.CategoryID)
	if err != nil {
		return nil, err
	}

	// Required category: always send
	if cat.DefaultState == "required" {
		if len(explicitChannels) > 0 {
			return explicitChannels, nil
		}
		return cat.DefaultChannels, nil
	}

	// Check explicit channel override — but respect opt-out
	channels := cat.DefaultChannels
	if len(explicitChannels) > 0 {
		channels = explicitChannels
	}

	// Check user subscription preference
	us, err := cr.store.GetUserSubscription(ctx, userID, sub.ID)
	if err == nil && us != nil {
		if !us.OptedIn {
			return nil, nil // user opted out
		}
		return channels, nil
	}

	// No explicit user preference — use category default state
	if cat.DefaultState == "off" {
		return nil, nil // default opt-out
	}

	// default state is "on"
	return channels, nil
}

// contactSkip records a channel dropped because the recipient lacks the
// channel's required contact address. AddressKey/AddressLabel are carried so
// the caller can reproduce today's exact log and event-reason strings.
type contactSkip struct {
	Channel      string
	AddressKey   string
	AddressLabel string
}

// filterChannelsByContact keeps only channels whose required contact point is
// present on the recipient. Channels with no address requirement (AddressKey
// "") are always kept, as are unknown channels (matching the prior switch,
// which had no default case). Returns kept channels and the skipped ones.
func filterChannelsByContact(channels []string, recipient hermenats.Recipient) (kept []string, skipped []contactSkip) {
	for _, ch := range channels {
		desc, ok := provider.Builtins.Channel(ch)
		if ok && desc.AddressKey != "" && recipient.AddressFor(desc.AddressKey) == "" {
			skipped = append(skipped, contactSkip{
				Channel:      ch,
				AddressKey:   desc.AddressKey,
				AddressLabel: desc.AddressLabel,
			})
			continue
		}
		kept = append(kept, ch)
	}
	return kept, skipped
}

// FilterChannelsForTemplate filters channels to only those with template
// content defined for them, per the channel registry. For direct sends (nil
// template), all channels pass through. Unknown channels are dropped.
func FilterChannelsForTemplate(channels []string, nt *models.NotificationTemplate) []string {
	if nt == nil {
		return channels
	}
	var filtered []string
	for _, ch := range channels {
		desc, ok := provider.Builtins.Channel(ch)
		if !ok || desc.HasContent == nil {
			continue
		}
		if desc.HasContent(nt) {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}
