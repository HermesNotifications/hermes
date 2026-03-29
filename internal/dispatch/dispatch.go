package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"

	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type Dispatch struct {
	nats             *messaging.Client
	store            store.NotificationRepository
	users            store.UserRepository
	tenants          store.TenantRepository
	templateResolver *TemplateResolver
	channelResolver  *ChannelResolver
	logger           *slog.Logger
}

func NewDispatch(nats *messaging.Client, store store.NotificationRepository, users store.UserRepository, tenants store.TenantRepository, templateResolver *TemplateResolver, channelResolver *ChannelResolver, logger *slog.Logger) *Dispatch {
	return &Dispatch{
		nats:             nats,
		store:            store,
		users:            users,
		tenants:          tenants,
		templateResolver: templateResolver,
		channelResolver:  channelResolver,
		logger:           logger,
	}
}

func (d *Dispatch) Start() error {
	return d.nats.Subscribe("notification.send", "dispatch", 256, 1, func(ctx context.Context, data []byte) error {
		return d.handleSend(ctx, data)
	})
}

func (d *Dispatch) handleSend(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalSend(data)
	if err != nil {
		d.logger.Error("unmarshal send message", "error", err)
		return fmt.Errorf("unmarshal send: %w", err)
	}

	log := d.logger.With("notification_id", msg.NotificationID)

	// Validate tenant
	if _, err := d.tenants.GetTenantByID(ctx, msg.TenantID); err != nil {
		log.Error("validate tenant", "error", err, "tenant_id", msg.TenantID)
		d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
			"error": "unknown tenant",
		})
		return fmt.Errorf("validate tenant: %w", err)
	}

	// Ensure user exists
	user, err := d.users.EnsureUser(ctx, msg.TenantID, msg.ExternalUserID)
	if err != nil {
		log.Error("ensure user", "error", err)
		return fmt.Errorf("ensure user: %w", err)
	}

	var nt *models.NotificationTemplate
	var rendered *RenderedContent
	var content hermenats.MessageContent
	if msg.Content != nil {
		content = *msg.Content
	}

	// Resolve category and subscription from template
	var categoryID string
	var subscriptionID string
	var templateID *string

	if msg.Metadata.Template != "" {
		// Template-based send: resolve template and render
		nt, err = d.templateResolver.Resolve(ctx, msg.Metadata.Template)
		if err != nil {
			log.Error("resolve template", "error", err, "template", msg.Metadata.Template)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("resolve template: %w", err)
		}

		templateID = &nt.ID
		if nt.SubscriptionID != nil {
			subscriptionID = *nt.SubscriptionID
			// Resolve category from subscription via channel resolver's store
			sub, subErr := d.channelResolver.store.GetSubscriptionByID(ctx, subscriptionID)
			if subErr == nil {
				categoryID = sub.CategoryID
			}
		}

		rendered, err = RenderTemplates(nt, msg.Data)
		if err != nil {
			log.Error("render templates", "error", err)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render templates: %w", err)
		}
	} else {
		// Direct send: optionally render content with data
		title, body, err := RenderDirectContent(content.Title, content.Body, msg.Data)
		if err != nil {
			log.Error("render direct content", "error", err)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render direct content: %w", err)
		}
		content.Title = title
		content.Body = body
	}

	// Create notification in DB
	channels := msg.Channels
	if channels == nil {
		channels = []string{}
	}
	n := &models.Notification{
		ID:         msg.NotificationID,
		TenantID:   msg.TenantID,
		UserID:     user.ID,
		TemplateID: templateID,
		CategoryID: categoryID,
		Channels:   channels,
		Status:     models.StatusPending,
	}

	if msg.Content != nil {
		n.Title = msg.Content.Title
		n.Body = msg.Content.Body
		n.ActionURL = msg.Content.ActionURL
		n.ActionLabel = msg.Content.ActionLabel
	} else if rendered != nil {
		// For template sends, use inbox title/body as the notification record content
		n.Title = rendered.InboxTitle
		n.Body = rendered.InboxBody
	}

	if msg.IdempotencyKey != "" {
		n.IdempotencyKey = &msg.IdempotencyKey
	}

	if _, err := d.store.CreateNotification(ctx, n); err != nil {
		// On retry: if notification already exists (unique constraint), fetch and continue
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "already exists") {
			log.Info("notification already exists (retry), continuing", "notification_id", msg.NotificationID)
		} else {
			log.Error("create notification", "error", err)
			return fmt.Errorf("create notification: %w", err)
		}
	}

	// Resolve channels
	if nt != nil {
		channels, err = d.channelResolver.ResolveChannels(ctx, msg.Channels, user.ID, nt)
	} else {
		// Direct send: use explicit channels
		channels = msg.Channels
	}
	if err != nil {
		log.Error("resolve channels", "error", err)
		d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
			"error": err.Error(),
		})
		return fmt.Errorf("resolve channels: %w", err)
	}

	// Filter channels by template content
	channels = FilterChannelsForTemplate(channels, nt)

	if len(channels) == 0 {
		log.Warn("no channels after filtering")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	// Resolve user contact info for recipient fields
	userFull, err := d.users.GetUserByID(ctx, user.ID)
	if err != nil {
		log.Error("resolve user", "error", err)
		return fmt.Errorf("resolve user: %w", err)
	}

	recipient := hermenats.Recipient{}
	if userFull.Email != nil {
		recipient.Email = *userFull.Email
	}
	if userFull.Phone != nil {
		recipient.Phone = *userFull.Phone
	}

	// Filter channels that require contact info the user doesn't have
	var filteredChannels []string
	for _, ch := range channels {
		switch ch {
		case "email":
			if recipient.Email == "" {
				log.Warn("skipping email channel: user has no email", "user_id", user.ID)
				d.publishEvent(ctx, msg.NotificationID, ch, "routing.no_contact", "warn", map[string]any{
					"reason": "user has no email address",
				})
				continue
			}
		case "sms":
			if recipient.Phone == "" {
				log.Warn("skipping sms channel: user has no phone", "user_id", user.ID)
				d.publishEvent(ctx, msg.NotificationID, ch, "routing.no_contact", "warn", map[string]any{
					"reason": "user has no phone number",
				})
				continue
			}
		}
		filteredChannels = append(filteredChannels, ch)
	}
	channels = filteredChannels

	if len(channels) == 0 {
		log.Warn("no channels after contact filtering")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	// Update notification channels in DB
	if err := d.store.UpdateNotificationChannels(ctx, msg.NotificationID, channels); err != nil {
		log.Error("update notification channels", "error", err)
		return fmt.Errorf("update notification channels: %w", err)
	}

	// Fan out to delivery channels
	for _, ch := range channels {
		deliveryContent := contentForChannel(ch, content, rendered)

		dm := &hermenats.DeliveryMessage{
			NotificationID: msg.NotificationID,
			TenantID:       msg.TenantID,
			UserID:         user.ID,
			Channel:        ch,
			Content:        deliveryContent,
			Metadata:       msg.Metadata,
			Recipient:      recipient,
			Attempt:        msg.Attempt,
		}

		dmBytes, err := dm.Marshal()
		if err != nil {
			log.Error("marshal delivery message", "error", err, "channel", ch)
			continue
		}

		subject := "delivery." + ch
		if err := d.nats.Publish(ctx, subject, dmBytes); err != nil {
			log.Error("publish delivery", "error", err, "channel", ch)
			d.publishEvent(ctx, msg.NotificationID, ch, "delivery.publish_failed", "error", map[string]any{
				"error": err.Error(),
			})
			continue
		}

		log.Info("published to delivery", "channel", ch)
		d.publishEvent(ctx, msg.NotificationID, ch, "routing.dispatched", "info", nil)
	}

	return nil
}

// contentForChannel returns the appropriate MessageContent for a given channel.
// For template-based sends, it uses the already-rendered templates.
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

func (d *Dispatch) publishEvent(ctx context.Context, notificationID, channel, event, severity string, metadata map[string]any) {
	em := &hermenats.EventMessage{
		NotificationID: notificationID,
		Channel:        channel,
		Event:          event,
		Severity:       severity,
		Metadata:       metadata,
	}
	emBytes, err := em.Marshal()
	if err != nil {
		d.logger.Error("marshal event message", "error", err)
		return
	}
	if err := d.nats.Publish(ctx, "notification.events", emBytes); err != nil {
		d.logger.Error("publish event", "error", err)
	}
}
