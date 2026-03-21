package router

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type ChannelResolver struct {
	store *store.Store
}

func NewChannelResolver(store *store.Store) *ChannelResolver {
	return &ChannelResolver{store: store}
}

// ResolveChannels determines target channels.
// Priority: explicit override → user preferences → group defaults.
func (cr *ChannelResolver) ResolveChannels(ctx context.Context, explicitChannels []string, userID, groupID string) ([]string, error) {
	if len(explicitChannels) > 0 {
		return explicitChannels, nil
	}

	pref, err := cr.store.GetUserPreference(ctx, userID, groupID)
	if err == nil && pref != nil && len(pref.Channels) > 0 {
		return pref.Channels, nil
	}

	group, err := cr.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return group.DefaultChannels, nil
}

// FilterChannelsForType filters channels to only those with templates defined.
// For direct sends (nil type), all channels pass through.
func FilterChannelsForType(channels []string, nt *models.NotificationType) []string {
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
