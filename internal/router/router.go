package router

import (
	"context"
	"fmt"
	"log/slog"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"

	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type Router struct {
	nats             *messaging.Client
	store            *store.Store
	templateResolver *TemplateResolver
	channelResolver  *ChannelResolver
	logger           *slog.Logger
}

func NewRouter(nats *messaging.Client, store *store.Store, templateResolver *TemplateResolver, channelResolver *ChannelResolver, logger *slog.Logger) *Router {
	return &Router{
		nats:             nats,
		store:            store,
		templateResolver: templateResolver,
		channelResolver:  channelResolver,
		logger:           logger,
	}
}

func (r *Router) Start() error {
	return r.nats.Subscribe("notification.send", "router", r.handleSend)
}

func (r *Router) handleSend(data []byte) error {
	msg, err := hermenats.UnmarshalSend(data)
	if err != nil {
		r.logger.Error("unmarshal send message", "error", err)
		return fmt.Errorf("unmarshal send: %w", err)
	}

	ctx := context.Background()
	log := r.logger.With("notification_id", msg.NotificationID)

	var nt *models.NotificationType
	var rendered *RenderedContent
	content := msg.Content

	if msg.Metadata.Type != "" {
		// Type-based send: resolve type and render templates
		nt, err = r.templateResolver.Resolve(ctx, msg.Metadata.Type)
		if err != nil {
			log.Error("resolve type", "error", err, "type", msg.Metadata.Type)
			r.publishEvent(msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("resolve type: %w", err)
		}

		rendered, err = RenderTemplates(nt, msg.Data)
		if err != nil {
			log.Error("render templates", "error", err)
			r.publishEvent(msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render templates: %w", err)
		}
	} else {
		// Direct send: optionally render content with data
		title, body, err := RenderDirectContent(content.Title, content.Body, msg.Data)
		if err != nil {
			log.Error("render direct content", "error", err)
			r.publishEvent(msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render direct content: %w", err)
		}
		content.Title = title
		content.Body = body
	}

	// Resolve channels
	channels, err := r.channelResolver.ResolveChannels(ctx, msg.Channels, msg.UserID, msg.GroupID)
	if err != nil {
		log.Error("resolve channels", "error", err)
		r.publishEvent(msg.NotificationID, "", "routing.failed", "error", map[string]any{
			"error": err.Error(),
		})
		return fmt.Errorf("resolve channels: %w", err)
	}

	// Filter channels by type templates
	channels = FilterChannelsForType(channels, nt)

	if len(channels) == 0 {
		log.Warn("no channels after filtering")
		r.publishEvent(msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	// Update notification channels in DB
	if err := r.store.UpdateNotificationChannels(ctx, msg.NotificationID, channels); err != nil {
		log.Error("update notification channels", "error", err)
		return fmt.Errorf("update notification channels: %w", err)
	}

	// Fan out to delivery channels
	for _, ch := range channels {
		deliveryContent := contentForChannel(ch, content, rendered)

		dm := &hermenats.DeliveryMessage{
			NotificationID: msg.NotificationID,
			TenantID:       msg.TenantID,
			UserID:         msg.UserID,
			Channel:        ch,
			Content:        deliveryContent,
			Metadata:       msg.Metadata,
			Attempt:        msg.Attempt,
		}

		dmBytes, err := dm.Marshal()
		if err != nil {
			log.Error("marshal delivery message", "error", err, "channel", ch)
			continue
		}

		subject := "delivery." + ch
		if err := r.nats.Publish(subject, dmBytes); err != nil {
			log.Error("publish delivery", "error", err, "channel", ch)
			r.publishEvent(msg.NotificationID, ch, "delivery.publish_failed", "error", map[string]any{
				"error": err.Error(),
			})
			continue
		}

		log.Info("published to delivery", "channel", ch)
		r.publishEvent(msg.NotificationID, ch, "routing.dispatched", "info", nil)
	}

	return nil
}

// contentForChannel returns the appropriate MessageContent for a given channel.
// For type-based sends, it uses the already-rendered templates.
// For direct sends (rendered == nil), it passes through the original content.
func contentForChannel(channel string, original hermenats.MessageContent, rendered *RenderedContent) hermenats.MessageContent {
	if rendered == nil {
		return original
	}

	mc := hermenats.MessageContent{
		ActionURL:   original.ActionURL,
		ActionLabel: original.ActionLabel,
	}

	switch channel {
	case "email":
		mc.Title = rendered.EmailSubject
		mc.Body = rendered.EmailBody
	case "sms":
		mc.Body = rendered.SMSBody
	case "inbox":
		mc.Title = rendered.InboxTitle
		mc.Body = rendered.InboxBody
	}

	return mc
}

func (r *Router) publishEvent(notificationID, channel, event, severity string, metadata map[string]any) {
	em := &hermenats.EventMessage{
		NotificationID: notificationID,
		Channel:        channel,
		Event:          event,
		Severity:       severity,
		Metadata:       metadata,
	}
	emBytes, err := em.Marshal()
	if err != nil {
		r.logger.Error("marshal event message", "error", err)
		return
	}
	if err := r.nats.Publish("notification.events", emBytes); err != nil {
		r.logger.Error("publish event", "error", err)
	}
}
