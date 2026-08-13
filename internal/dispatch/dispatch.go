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
	"github.com/hermesnotifications/hermes/internal/provider"
	"github.com/hermesnotifications/hermes/internal/store"
)

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

	// insertBatchSize and insertLinger configure the batcher that Start creates. Held as
	// settings rather than applied here because the useful batch size is bounded by the worker
	// count, and only Start is told what that is.
	insertBatchSize int
	insertLinger    time.Duration
	// inserts is nil when batching is off, in which case each notification is written by its
	// own statement exactly as before. Written by Start, before any handler can run.
	inserts *insertBatcher
}

// Option configures a Dispatch at construction.
type Option func(*Dispatch)

// WithInsertBatching sets how many notifications may share one insert transaction, and how long
// an unfilled batch is held open waiting for more.
//
// A size of 1 or less turns batching off and restores the one-transaction-per-notification write
// path — the kill switch, if a deployment ever needs to rule this mechanism out. A linger of 0
// (the default) does not disable anything: batches are assembled from rows that are already
// waiting, which is where the throughput comes from. See the note atop insertbatch.go.
func WithInsertBatching(size int, linger time.Duration) Option {
	return func(d *Dispatch) {
		d.insertBatchSize = size
		d.insertLinger = linger
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
		insertBatchSize:  defaultInsertBatchSize,
		insertLinger:     defaultInsertLinger,
	}
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
	d.startInsertBatcher(workers)
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

// startInsertBatcher brings up the insert batcher, capping it at the worker count.
//
// The cap is arithmetic rather than caution. Every producer is a pool worker blocked inside
// Submit until its row is committed, so at most `workers` rows can be waiting at any instant and
// a larger size is unreachable — it would only mislead the batch-size metric into looking as
// though the size knob were the thing limiting the batch.
func (d *Dispatch) startInsertBatcher(workers int) {
	size := min(d.insertBatchSize, workers)
	if size <= 1 {
		// A batch of one is the unbatched path with a goroutine and a channel bolted on.
		return
	}
	if size < d.insertBatchSize {
		d.logger.Info("insert batch size capped at the dispatch worker count",
			"requested", d.insertBatchSize, "workers", workers, "effective", size,
			"hint", "raise HERMES_DISPATCH_CONCURRENCY to batch more rows per transaction")
	}
	d.inserts = newInsertBatcher(d.store, size, d.insertLinger, d.logger)
	go d.inserts.run()
}

// Stop shuts the insert batcher down, writing whatever it still holds.
//
// Call it *after* the NATS drain, never before: handlers parked in Submit can only finish while
// the batcher is still running, and the drain is what waits for them. Stopping first would leave
// them to be released with errBatcherStopped and their messages redelivered.
func (d *Dispatch) Stop() {
	if d.inserts != nil {
		d.inserts.stopAndWait()
	}
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

	if _, err := d.organizations.EnsureOrganization(ctx, msg.OrganizationID); err != nil {
		log.Error("ensure organization", "error", err, "organization_id", msg.OrganizationID)
		if isPermanentDBError(err) {
			return permanent(fmt.Errorf("ensure organization: %w", err))
		}
		return d.transientOrGiveUp(ctx, log, msg.NotificationID, info, fmt.Errorf("ensure organization: %w", err))
	}

	user, err := d.users.EnsureUser(ctx, msg.OrganizationID, msg.ExternalUserID)
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

	if err := d.persistNotification(ctx, n); err != nil {
		if isDuplicateNotification(err) {
			log.Info("notification already exists (retry), continuing")
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

// persistNotification writes the record that every later phase updates, through the insert
// batcher when one is running.
//
// It returns only once the row is durably committed — or once its failure is known — either
// way. That is what keeps the ack in internal/messaging behind durability whichever path is
// taken; see insertbatch.go.
func (d *Dispatch) persistNotification(ctx context.Context, n *models.Notification) error {
	if d.inserts != nil {
		return d.inserts.Submit(ctx, n)
	}
	_, err := d.store.CreateNotification(ctx, n)
	return err
}

// isDuplicateNotification reports whether the insert failed only because the notification was
// already persisted — the normal outcome when a message is redelivered, since dispatch reuses
// the notification ID the Send service minted.
//
// The string matching is for the single-row path, where the duplicate arrives as whatever the
// backend's driver produced (a pgx unique-violation, DynamoDB's condition-check failure). The
// batch path skips such rows inside the transaction instead of failing them, and reports them
// as errAlreadyExists.
func isDuplicateNotification(err error) bool {
	if errors.Is(err, errAlreadyExists) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique") ||
		strings.Contains(msg, "already exists")
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
		var err error
		nt, err = d.templateResolver.Resolve(ctx, msg.Metadata.Template)
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
		log.Warn("no channels after filtering")
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
		log.Warn(fmt.Sprintf("skipping %s channel: user has no %s", s.Channel, s.AddressKey), "user_id", user.ID)
		d.publishEvent(ctx, msg.NotificationID, s.Channel, "routing.no_contact", "warn", map[string]any{
			"reason": "user has no " + s.AddressLabel,
		})
	}
	channels = filteredChannels

	if len(channels) == 0 {
		log.Warn("no channels after contact filtering")
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
