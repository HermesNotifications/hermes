package dispatch

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/models"
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
}

func NewChannelResolver(store channelStore) *ChannelResolver {
	return &ChannelResolver{store: store}
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
		return nil, nil
	}

	// Template with subscription — resolve category
	sub, err := cr.store.GetSubscriptionByID(ctx, *template.SubscriptionID)
	if err != nil {
		return nil, err
	}
	cat, err := cr.store.GetCategoryByID(ctx, sub.CategoryID)
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

// FilterChannelsForTemplate filters channels to only those with templates defined.
// For direct sends (nil template), all channels pass through.
func FilterChannelsForTemplate(channels []string, nt *models.NotificationTemplate) []string {
	if nt == nil {
		return channels
	}
	var filtered []string
	for _, ch := range channels {
		switch ch {
		case "email":
			if nt.EmailSubject != nil || nt.EmailBody != nil {
				filtered = append(filtered, ch)
			}
		case "sms":
			if nt.SMSBody != nil {
				filtered = append(filtered, ch)
			}
		case "inbox":
			if nt.InboxTitle != nil || nt.InboxBody != nil {
				filtered = append(filtered, ch)
			}
		}
	}
	return filtered
}
