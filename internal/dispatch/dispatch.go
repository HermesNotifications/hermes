// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	hermenats "github.com/hermesnotifications/hermes/internal/nats"

	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/observability"
	"github.com/hermesnotifications/hermes/internal/provider"
	"github.com/hermesnotifications/hermes/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracer names spans after this package's import path, per
// docs/observability/instrumentation-guide.md.
var tracer = observability.Tracer("github.com/hermesnotifications/hermes/internal/dispatch")

// eventNotificationSent is published once per notification, after the fan-out, and is the
// only event that advances a notification to StatusSent. The string is a cross-package
// contract with eventwriter.eventToStatus, which is why it is named rather than inlined.
const eventNotificationSent = "notification.sent"

// bus is the slice of the NATS client dispatch uses. Declared as an interface so the fan-out
// and the events that accompany it can be exercised without a broker; *messaging.Client
// satisfies it, and cmd/dispatch still passes one.
type bus interface {
	Publish(ctx context.Context, subject string, data []byte) error
	Subscribe(cfg messaging.SubscribeConfig, handler func(ctx context.Context, data []byte, info messaging.DeliveryInfo) error) error
}

type Dispatch struct {
	nats             bus
	store            store.NotificationRepository
	users            store.UserRepository
	organizations    store.OrganizationRepository
	templateResolver *TemplateResolver
	channelResolver  *ChannelResolver
	logger           *slog.Logger

	// The two upserts that precede every notification insert, memoized per replica.
	// Both may be nil, which disables them — see identity_cache.go for what is cached,
	// what deliberately is not, and why this is in-process rather than in Redis.
	organizationsSeen *lruCache[string, struct{}]
	usersByExternalID *lruCache[userKey, cachedUser]
}

// Option adjusts a Dispatch at construction. Introduced so the identity caches could be
// sized without growing the positional parameter list a sixth caller would have to thread
// a value through.
type Option func(*Dispatch)

// WithIdentityCache sizes the per-replica organization and user caches. A size of zero or
// less turns them off, which restores the pre-cache behaviour of one upsert per message.
func WithIdentityCache(size int) Option {
	return func(d *Dispatch) {
		d.organizationsSeen = newLRUCache[string, struct{}](size)
		d.usersByExternalID = newLRUCache[userKey, cachedUser](size)
	}
}

func NewDispatch(nats bus, store store.NotificationRepository, users store.UserRepository, organizations store.OrganizationRepository, templateResolver *TemplateResolver, channelResolver *ChannelResolver, logger *slog.Logger, opts ...Option) *Dispatch {
	d := &Dispatch{
		nats:             nats,
		store:            store,
		users:            users,
		organizations:    organizations,
		templateResolver: templateResolver,
		channelResolver:  channelResolver,
		logger:           logger,
	}
	// Caching is the default rather than opt-in: it is a straight win for every caller,
	// and the callers that do not read config (the e2e harness, cmd/dispatchbench) are
	// exactly the ones whose measurements should reflect what production runs.
	WithIdentityCache(DefaultIdentityCacheSize)(d)
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Start begins consuming notification.send with a pool of `workers` processing
// notifications in parallel, backed by a `prefetch`-deep fetch buffer. Distinct
// notifications are independent — each carries its own record and status rollup
// is monotonic downstream — so they can be processed concurrently to lift
// dispatch throughput.
func (d *Dispatch) Start(workers, prefetch int) error {
	return d.nats.Subscribe(messaging.SubscribeConfig{
		Subject:       "notification.send",
		Consumer:      "dispatch",
		MaxAckPending: 256,
		Workers:       workers,
		Prefetch:      prefetch,
		// Persisting the notification, resolving a template and fanning out is database and
		// Redis work, so this is generous rather than tight — but bounded, so a saturated pool
		// waiting on a connection cannot pin every worker indefinitely.
		HandlerTimeout: 30 * time.Second,
		AckWait:        60 * time.Second,
	}, func(ctx context.Context, data []byte, info messaging.DeliveryInfo) error {
		return d.handleSend(ctx, data, info)
	})
}

// permanentError wraps an error that should not be retried.
// It implements messaging.PermanentError so the NATS subscriber acks instead of nacking.
type permanentError struct{ err error }

func (e *permanentError) Error() string   { return e.err.Error() }
func (e *permanentError) Unwrap() error   { return e.err }
func (e *permanentError) Permanent() bool { return true }

func permanent(err error) error { return &permanentError{err: err} }

func (d *Dispatch) handleSend(ctx context.Context, data []byte, info messaging.DeliveryInfo) error {
	msg, err := hermenats.UnmarshalSend(data)
	if err != nil {
		d.logger.Error("unmarshal send message", "error", err)
		return permanent(fmt.Errorf("unmarshal send: %w", err))
	}

	log := d.logger.With("notification_id", msg.NotificationID, "attempt", info.Attempt)

	// --- Phase 1: Ensure organization + user, create notification record early ---
	// This guarantees a DB record exists for troubleshooting even if later steps fail.
	// Failures here are transient (DB down) and retryable, but we give up on last attempt.

	if err := d.ensureOrganization(ctx, msg.OrganizationID); err != nil {
		log.Error("ensure organization", "error", err, "organization_id", msg.OrganizationID)
		if isPermanentDBError(err) {
			return permanent(fmt.Errorf("ensure organization: %w", err))
		}
		return d.transientOrGiveUp(ctx, log, msg.NotificationID, info, fmt.Errorf("ensure organization: %w", err))
	}

	user, err := d.ensureUser(ctx, msg.OrganizationID, msg.ExternalUserID)
	if err != nil {
		log.Error("ensure user", "error", err)
		return d.transientOrGiveUp(ctx, log, msg.NotificationID, info, fmt.Errorf("ensure user: %w", err))
	}

	// Build a minimal notification record so it's persisted before any routing logic.
	channels := msg.Channels
	if channels == nil {
		channels = []string{}
	}
	n := &models.Notification{
		ID:             msg.NotificationID,
		OrganizationID: msg.OrganizationID,
		UserID:         user.ID,
		Channels:       channels,
		Status:         models.StatusPending,
		Metadata:       msg.ClientMetadata,
	}
	if msg.Content != nil {
		n.Title = msg.Content.Title
		n.Body = msg.Content.Body
		n.ActionURL = msg.Content.ActionURL
		n.ActionLabel = msg.Content.ActionLabel
	}
	if msg.IdempotencyKey != "" {
		n.IdempotencyKey = &msg.IdempotencyKey
	}

	if _, err := d.store.CreateNotification(ctx, n); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "already exists") {
			// Debug: this is the idempotency guard doing its job on a redelivery.
			// It is expected traffic on any NATS retry, and on a retry storm it was
			// one Info per redelivered message on top of everything else.
			log.Debug("notification already exists (retry), continuing")
		} else {
			log.Error("create notification", "error", err)
			return d.transientOrGiveUp(ctx, log, msg.NotificationID, info, fmt.Errorf("create notification: %w", err))
		}
	}

	// --- Phase 2: Resolve templates, render, route ---
	// Failures here are permanent (bad template, bad data) and recorded against the notification.

	if err := d.routeAndDeliver(ctx, log, msg, n, user); err != nil {
		d.failNotification(ctx, log, msg.NotificationID, err, classifyError(err))
		return permanent(err)
	}

	return nil
}

// isPermanentDBError reports whether a Postgres error is in class 22 (Data Exception)
// or class 23 (Integrity Constraint Violation). These indicate malformed input that
// will never succeed on retry and should be treated as permanent failures.
func isPermanentDBError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		class := pgErr.Code
		if len(class) >= 2 {
			switch class[:2] {
			case "22", // Data Exception (e.g. 22P02 invalid UUID format)
				"23": // Integrity Constraint Violation
				return true
			}
		}
	}
	return false
}

// transientOrGiveUp returns a transient error for retry, unless this is the last attempt,
// in which case it marks the notification as failed and returns a permanent error to stop retries.
func (d *Dispatch) transientOrGiveUp(ctx context.Context, log *slog.Logger, notificationID string, info messaging.DeliveryInfo, err error) error {
	if !info.LastAttempt {
		return err // transient — will be retried with backoff
	}
	log.Error("giving up after max retries", "error", err)
	d.failNotification(ctx, log, notificationID, err, "routing.failed")
	return permanent(err)
}

// failNotification marks a notification as failed and emits a typed failure event.
func (d *Dispatch) failNotification(ctx context.Context, log *slog.Logger, notificationID string, reason error, event string) {
	d.publishEvent(ctx, notificationID, "", event, "error", map[string]any{
		"error": reason.Error(),
	})
	if err := d.store.FailNotification(ctx, notificationID); err != nil {
		log.Error("mark notification failed", "error", err)
	}
}

// classifyError returns the appropriate event type for a routing/rendering error.
func classifyError(err error) string {
	msg := err.Error()
	if strings.HasPrefix(msg, "render templates:") || strings.HasPrefix(msg, "render direct content:") {
		return "render.failed"
	}
	if strings.HasPrefix(msg, "resolve template:") {
		return "template.not_found"
	}
	return "routing.failed"
}

// routeAndDeliver handles template resolution, rendering, channel routing, and fan-out.
// Errors returned from this function are permanent (will not succeed on retry).
func (d *Dispatch) routeAndDeliver(ctx context.Context, log *slog.Logger, msg *hermenats.SendMessage, n *models.Notification, user *models.User) error {
	var nt *models.NotificationTemplate
	var rendered RenderedContent
	var content hermenats.MessageContent
	if msg.Content != nil {
		content = *msg.Content
	}

	var categoryID string
	var subscriptionID string
	var templateID *string

	if msg.Metadata.Template != "" {
		// Worth its own span: the resolver is Redis-backed, so a cache hit is
		// invisible in the trace -- the DB query span only appears on a miss, which
		// made "slow because the cache is cold" and "slow for some other reason"
		// look identical.
		resolveCtx, resolveSpan := tracer.Start(ctx, "template.resolve",
			trace.WithAttributes(attribute.String("template.slug", msg.Metadata.Template)))
		var err error
		nt, err = d.templateResolver.Resolve(resolveCtx, msg.Metadata.Template)
		observability.RecordError(resolveSpan, err)
		resolveSpan.End()
		if err != nil {
			log.Error("resolve template", "error", err, "template", msg.Metadata.Template)
			return fmt.Errorf("resolve template: %w", err)
		}

		templateID = &nt.ID
		if nt.SubscriptionID != nil {
			subscriptionID = *nt.SubscriptionID
			sub, subErr := d.channelResolver.store.GetSubscriptionByID(ctx, subscriptionID)
			if subErr == nil {
				categoryID = sub.CategoryID
			}
		}

		rendered, err = RenderTemplates(nt, msg.Data)
		if err != nil {
			log.Error("render templates", "error", err)
			return fmt.Errorf("render templates: %w", err)
		}
	} else {
		title, body, err := RenderDirectContent(content.Title, content.Body, msg.Data)
		if err != nil {
			log.Error("render direct content", "error", err)
			return fmt.Errorf("render direct content: %w", err)
		}
		content.Title = title
		content.Body = body
	}

	// Backfill notification record with resolved template/rendered data
	needsUpdate := templateID != nil || categoryID != "" || rendered != nil
	if !needsUpdate && len(msg.Data) > 0 && msg.Content != nil {
		// Direct content was rendered with data — write resolved content back
		needsUpdate = true
	}
	if needsUpdate {
		update := &models.Notification{ID: n.ID, TemplateID: templateID, CategoryID: categoryID}
		if rendered != nil {
			update.Title, update.Body = projectContent(provider.ChannelInbox, rendered)
		} else if len(msg.Data) > 0 {
			update.Title = content.Title
			update.Body = content.Body
		}
		if err := d.store.UpdateNotificationRouting(ctx, update); err != nil {
			log.Error("update notification routing", "error", err)
			// Non-fatal: the notification record exists, routing data is nice-to-have
		}
	}

	// Resolve channels
	channels := msg.Channels
	var err error
	if nt != nil {
		channels, err = d.channelResolver.ResolveChannels(ctx, msg.Channels, user.ID, nt)
	}
	if err != nil {
		log.Error("resolve channels", "error", err)
		return fmt.Errorf("resolve channels: %w", err)
	}

	channels = FilterChannelsForTemplate(channels, nt)

	if len(channels) == 0 {
		// Debug, not Warn. Resolving to no channels is a routine outcome of the
		// rules working — a category the user opted out of, or a template with no
		// content for any channel they can receive — not a fault anyone can act on
		// per occurrence. It stays visible in aggregate through routingDrops, and
		// per notification through the durable routing.no_channels event below.
		log.Debug("no channels after template filtering")
		recordRoutingDrop(ctx, "", "no_channels_for_template")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	recipient := hermenats.Recipient{}
	for k, v := range user.Contacts {
		recipient[k] = v
	}
	for k, v := range msg.Contacts {
		if v != "" {
			recipient[k] = v
		}
	}

	// Filter channels that require contact info (per the channel registry).
	filteredChannels, skipped := filterChannelsByContact(channels, recipient)
	for _, s := range skipped {
		// The message was interpolated per channel, which made "msg" a variable
		// string and defeated grouping on it in Loki — semantic-conventions.md asks
		// for a short constant msg with the specifics as attributes. Level follows
		// the same reasoning as the drop above: a user without a phone number is
		// the expected state for most users, not a warning.
		log.Debug("skipping channel: recipient has no contact point",
			"channel", s.Channel, "address_key", s.AddressKey, "user_id", user.ID)
		recordRoutingDrop(ctx, s.Channel, "no_contact")
		d.publishEvent(ctx, msg.NotificationID, s.Channel, "routing.no_contact", "warn", map[string]any{
			"reason": "user has no " + s.AddressLabel,
		})
	}
	channels = filteredChannels

	if len(channels) == 0 {
		log.Debug("no channels after contact filtering")
		recordRoutingDrop(ctx, "", "no_contact_for_any_channel")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	if err := d.store.UpdateNotificationChannels(ctx, msg.NotificationID, channels); err != nil {
		log.Error("update notification channels", "error", err)
		return fmt.Errorf("update notification channels: %w", err)
	}

	// Fan out to delivery channels
	var dispatched []string
	for _, ch := range channels {
		deliveryContent := contentForChannel(ch, content, rendered)

		dm := &hermenats.DeliveryMessage{
			NotificationID: msg.NotificationID,
			OrganizationID: msg.OrganizationID,
			UserID:         user.ID,
			Channel:        ch,
			Content:        deliveryContent,
			Metadata:       msg.Metadata,
			ClientMetadata: msg.ClientMetadata,
			Recipient:      recipient,
			Attempt:        msg.Attempt,
		}

		dmBytes, err := dm.Marshal()
		if err != nil {
			log.Error("marshal delivery message", "error", err, "channel", ch)
			recordDispatchFailure(ctx, ch, "marshal")
			continue
		}

		subject := "delivery." + ch
		if err := d.nats.Publish(ctx, subject, dmBytes); err != nil {
			log.Error("publish delivery", "error", err, "channel", ch)
			recordDispatchFailure(ctx, ch, "publish")
			d.publishEvent(ctx, msg.NotificationID, ch, "delivery.publish_failed", "error", map[string]any{
				"error": err.Error(),
			})
			continue
		}

		// Debug: one record per channel per notification, restating what the
		// routing.dispatched event on the next line already records durably and what
		// the delivery.* subject depth shows in aggregate. The failure paths above
		// stay at Error, which is the asymmetry worth keeping — a publish that works
		// is not news, a publish that does not is.
		log.Debug("published to delivery", "channel", ch)
		recordDispatched(ctx, ch)
		dispatched = append(dispatched, ch)
		d.publishEvent(ctx, msg.NotificationID, ch, "routing.dispatched", "info", nil)
	}

	// The hand-off to the channels is what "sent" means, and this is the event that records
	// it. eventwriter.eventToStatus has always mapped notification.sent to StatusSent, but
	// nothing published it: dispatch emitted only the per-channel routing.dispatched, which
	// maps to no status. Rank 1 of the status ladder was therefore dead, and a notification
	// jumped pending -> delivered on the first worker's "<channel>.sent".
	//
	// Published after the fan-out, and only when a delivery message actually reached the bus:
	// a notification nothing was handed to has not been sent, and advancing it to "sent"
	// would promise a delivery no worker will ever attempt.
	if len(dispatched) > 0 {
		d.publishEvent(ctx, msg.NotificationID, "", eventNotificationSent, "info", map[string]any{
			"channels": dispatched,
		})
	}

	return nil
}

// projectContent returns the title and body that a channel's content schema maps
// its rendered fields onto.
func projectContent(channel string, rendered RenderedContent) (title, body string) {
	desc, ok := provider.Builtins.Channel(channel)
	if !ok {
		return "", ""
	}
	fields := rendered[channel]
	for _, f := range desc.Content {
		switch f.MapsTo {
		case "title":
			title = fields[f.Key]
		case "body":
			body = fields[f.Key]
		}
	}
	return title, body
}

// contentForChannel returns the MessageContent for a channel. For template sends
// it projects the rendered per-channel content; for direct sends (rendered nil)
// it passes through the original content.
func contentForChannel(channel string, original hermenats.MessageContent, rendered RenderedContent) hermenats.MessageContent {
	if rendered == nil {
		return original
	}
	mc := hermenats.MessageContent{
		ActionURL:   original.ActionURL,
		ActionLabel: original.ActionLabel,
	}
	mc.Title, mc.Body = projectContent(channel, rendered)
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
