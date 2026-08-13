// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/models"
	hermenats "github.com/hermesnotifications/hermes/internal/nats"
)

// The "sent" status was unreachable. eventwriter.eventToStatus maps "notification.sent" to
// StatusSent, but dispatch only ever published per-channel "routing.dispatched" events, which
// map to nothing — so a notification went straight from pending to delivered on the first
// worker's "<channel>.sent", and the rank-1 rung of the status ladder was dead. These tests
// pin the hand-off event so the two halves of the contract cannot drift apart again.

// --- fakes -------------------------------------------------------------------------------

type publishedMsg struct {
	Subject string
	Data    []byte
}

// fakeBus records publishes instead of putting them on a bus. Subscribe is never called by
// the handler path under test.
type fakeBus struct {
	published []publishedMsg
	failOn    map[string]error // subject -> error to return
}

func (f *fakeBus) Publish(_ context.Context, subject string, data []byte) error {
	if err, ok := f.failOn[subject]; ok {
		return err
	}
	f.published = append(f.published, publishedMsg{Subject: subject, Data: data})
	return nil
}

func (f *fakeBus) Subscribe(messaging.SubscribeConfig, func(context.Context, []byte, messaging.DeliveryInfo) error) error {
	return nil
}

// events returns every EventMessage published to notification.events.
func (f *fakeBus) events(t *testing.T) []hermenats.EventMessage {
	t.Helper()
	var out []hermenats.EventMessage
	for _, m := range f.published {
		if m.Subject != "notification.events" {
			continue
		}
		var e hermenats.EventMessage
		if err := json.Unmarshal(m.Data, &e); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// eventNames returns the event names published to notification.events, in order.
func (f *fakeBus) eventNames(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, e := range f.events(t) {
		out = append(out, e.Event)
	}
	return out
}

// deliverySubjects returns the subjects of every delivery fan-out publish, in order.
func (f *fakeBus) deliverySubjects() []string {
	var out []string
	for _, m := range f.published {
		if m.Subject != "notification.events" {
			out = append(out, m.Subject)
		}
	}
	return out
}

// deliveries returns every DeliveryMessage fanned out, in order.
func (f *fakeBus) deliveries(t *testing.T) []hermenats.DeliveryMessage {
	t.Helper()
	var out []hermenats.DeliveryMessage
	for _, m := range f.published {
		if m.Subject == "notification.events" {
			continue
		}
		var dm hermenats.DeliveryMessage
		if err := json.Unmarshal(m.Data, &dm); err != nil {
			t.Fatalf("unmarshal delivery message: %v", err)
		}
		out = append(out, dm)
	}
	return out
}

type fakeNotifStore struct {
	created  *models.Notification
	batches  [][]string // notification IDs per CreateNotifications call
	channels []string
	failed   bool
}

func (f *fakeNotifStore) CreateNotification(_ context.Context, n *models.Notification) (*models.Notification, error) {
	f.created = n
	return n, nil
}

func (f *fakeNotifStore) CreateNotifications(_ context.Context, ns []*models.Notification) ([]string, error) {
	ids := make([]string, 0, len(ns))
	for _, n := range ns {
		f.created = n
		ids = append(ids, n.ID)
	}
	f.batches = append(f.batches, ids)
	return ids, nil
}
func (f *fakeNotifStore) UpdateNotificationChannels(_ context.Context, _ string, channels []string) error {
	f.channels = channels
	return nil
}
func (f *fakeNotifStore) UpdateNotificationRouting(context.Context, *models.Notification) error {
	return nil
}
func (f *fakeNotifStore) FailNotification(context.Context, string) error {
	f.failed = true
	return nil
}
func (f *fakeNotifStore) GetNotificationByID(context.Context, string) (*models.Notification, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeNotifStore) GetNotificationByIdempotencyKey(context.Context, string, string) (*models.Notification, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeNotifStore) GetNotificationEvents(context.Context, string) ([]models.NotificationEvent, error) {
	return nil, nil
}
func (f *fakeNotifStore) ListRecentNotifications(context.Context, int) ([]models.Notification, error) {
	return nil, nil
}

type fakeUserStore struct{ user *models.User }

func (f *fakeUserStore) EnsureUser(context.Context, string, string) (*models.User, error) {
	return f.user, nil
}
func (f *fakeUserStore) GetUserByID(context.Context, string) (*models.User, error) {
	return f.user, nil
}
func (f *fakeUserStore) UpdateUserContacts(context.Context, string, *string, *string) (*models.User, error) {
	return f.user, nil
}
func (f *fakeUserStore) ListUsers(context.Context, string) ([]models.User, error) { return nil, nil }
func (f *fakeUserStore) GetUserContacts(context.Context, string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeUserStore) SetUserContact(context.Context, string, string, string) error { return nil }

type fakeOrgStore struct{}

func (f *fakeOrgStore) EnsureOrganization(_ context.Context, id string) (*models.Organization, error) {
	return &models.Organization{ID: id}, nil
}
func (f *fakeOrgStore) CreateOrganization(context.Context, string, string) (*models.Organization, error) {
	return nil, nil
}
func (f *fakeOrgStore) GetOrganizationByID(context.Context, string) (*models.Organization, error) {
	return nil, nil
}
func (f *fakeOrgStore) ListOrganizations(context.Context) ([]models.Organization, error) {
	return nil, nil
}
func (f *fakeOrgStore) CountUsersByOrganization(context.Context) (map[string]int, error) {
	return nil, nil
}

// newTestDispatch wires a Dispatch over fakes. The template and channel resolvers are nil:
// every case here is a direct-content send, which does not consult either.
func newTestDispatch(bus *fakeBus, notifs *fakeNotifStore, user *models.User) *Dispatch {
	return &Dispatch{
		nats:          bus,
		store:         notifs,
		users:         &fakeUserStore{user: user},
		organizations: &fakeOrgStore{},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// directSend builds a direct-content SendMessage for the given channels.
func directSend(channels ...string) []byte {
	msg := &hermenats.SendMessage{
		NotificationID: "ntf_test",
		OrganizationID: "org_test",
		ExternalUserID: "ext_1",
		Content:        &hermenats.MessageContent{Title: "hi", Body: "there"},
		Channels:       channels,
		Attempt:        1,
	}
	data, _ := msg.Marshal()
	return data
}

// directSendWithMetadata is directSend plus a client metadata object.
func directSendWithMetadata(metadata models.NotificationMetadata, channels ...string) []byte {
	msg := &hermenats.SendMessage{
		NotificationID: "ntf_test",
		OrganizationID: "org_test",
		ExternalUserID: "ext_1",
		Content:        &hermenats.MessageContent{Title: "hi", Body: "there"},
		ClientMetadata: metadata,
		Channels:       channels,
		Attempt:        1,
	}
	data, _ := msg.Marshal()
	return data
}

func firstAttempt() messaging.DeliveryInfo {
	return messaging.DeliveryInfo{Attempt: 1, LastAttempt: false}
}

func countEvent(names []string, want string) int {
	n := 0
	for _, got := range names {
		if got == want {
			n++
		}
	}
	return n
}

// --- tests -------------------------------------------------------------------------------

// Metadata has to land in two places from one message: on the row that dispatch persists
// before any routing runs, and on every delivery message it fans out. Only the second reaches
// the inbox worker, which has no database and so cannot recover it any other way.
func TestHandleSend_MetadataIsPersistedAndFannedOut(t *testing.T) {
	bus := &fakeBus{}
	notifs := &fakeNotifStore{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, notifs, user)

	metadata := models.NotificationMetadata{"level": "error", "toast": true, "ref": "abc"}
	send := directSendWithMetadata(metadata, "email", "inbox")
	if err := d.handleSend(context.Background(), send, firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	if notifs.created == nil {
		t.Fatal("no notification was created")
	}
	if level, ok := notifs.created.Metadata.Level(); !ok || level != "error" {
		t.Errorf("persisted level = (%q, %v), want (\"error\", true)", level, ok)
	}
	if !notifs.created.Metadata.Toast() {
		t.Error("persisted metadata lost the toast flag")
	}
	if notifs.created.Metadata["ref"] != "abc" {
		t.Errorf("persisted metadata lost the opaque key: %#v", notifs.created.Metadata["ref"])
	}

	deliveries := bus.deliveries(t)
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 delivery messages, got %d", len(deliveries))
	}
	for _, dm := range deliveries {
		if level, ok := dm.ClientMetadata.Level(); !ok || level != "error" {
			t.Errorf("%s delivery lost the level: %#v", dm.Channel, dm.ClientMetadata)
		}
		if dm.ClientMetadata["ref"] != "abc" {
			t.Errorf("%s delivery lost the opaque key: %#v", dm.Channel, dm.ClientMetadata)
		}
	}
}

func TestHandleSend_NoMetadataStaysNil(t *testing.T) {
	bus := &fakeBus{}
	notifs := &fakeNotifStore{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, notifs, user)

	if err := d.handleSend(context.Background(), directSend("inbox"), firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	if notifs.created == nil {
		t.Fatal("no notification was created")
	}
	if notifs.created.Metadata != nil {
		t.Errorf("metadata invented for a send that carried none: %#v", notifs.created.Metadata)
	}
	for _, dm := range bus.deliveries(t) {
		if dm.ClientMetadata != nil {
			t.Errorf("%s delivery invented metadata: %#v", dm.Channel, dm.ClientMetadata)
		}
	}
}

// A successful fan-out must publish exactly one notification.sent, which is what advances the
// notification to the "sent" status. Without it the status ladder skips rank 1 entirely.
func TestHandleSend_PublishesNotificationSentAfterFanOut(t *testing.T) {
	bus := &fakeBus{}
	notifs := &fakeNotifStore{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, notifs, user)

	if err := d.handleSend(context.Background(), directSend("email", "inbox"), firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	names := bus.eventNames(t)
	if got := countEvent(names, "notification.sent"); got != 1 {
		t.Fatalf("notification.sent count: got %d, want 1 (events: %v)", got, names)
	}
	if got := countEvent(names, "routing.dispatched"); got != 2 {
		t.Errorf("routing.dispatched count: got %d, want 2 (events: %v)", got, names)
	}
}

// The hand-off event must name the channels it was actually handed to, and must carry no
// channel of its own — it is about the notification, not one delivery.
func TestHandleSend_NotificationSentCarriesDispatchedChannels(t *testing.T) {
	bus := &fakeBus{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, &fakeNotifStore{}, user)

	if err := d.handleSend(context.Background(), directSend("email", "inbox"), firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	var sent *hermenats.EventMessage
	for _, e := range bus.events(t) {
		if e.Event == "notification.sent" {
			sent = &e
			break
		}
	}
	if sent == nil {
		t.Fatal("no notification.sent event published")
	}
	if sent.Channel != "" {
		t.Errorf("Channel: got %q, want empty", sent.Channel)
	}
	if sent.Severity != "info" {
		t.Errorf("Severity: got %q, want %q", sent.Severity, "info")
	}
	got, _ := sent.Metadata["channels"].([]any)
	want := []string{"email", "inbox"}
	if len(got) != len(want) {
		t.Fatalf("metadata channels: got %v, want %v", sent.Metadata["channels"], want)
	}
	for i, ch := range want {
		if got[i] != ch {
			t.Errorf("metadata channels[%d]: got %v, want %q", i, got[i], ch)
		}
	}
}

// The hand-off event must follow the fan-out because it reports what the fan-out achieved:
// which channels reached the bus is not known until the loop has run. This is NOT about the
// status rollup — eventwriter dedups a batch by rank, not by arrival order, so publishing
// earlier would not change any status. It is about not claiming a hand-off that has not
// happened yet, which is the same invariant the no-dispatch cases below pin from the other side.
func TestHandleSend_NotificationSentPublishedAfterDeliveryFanOut(t *testing.T) {
	bus := &fakeBus{}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, &fakeNotifStore{}, user)

	if err := d.handleSend(context.Background(), directSend("email"), firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	sentIdx, deliveryIdx := -1, -1
	for i, m := range bus.published {
		if m.Subject == "delivery.email" {
			deliveryIdx = i
		}
		if m.Subject == "notification.events" {
			var e hermenats.EventMessage
			if err := json.Unmarshal(m.Data, &e); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if e.Event == "notification.sent" {
				sentIdx = i
			}
		}
	}
	if deliveryIdx == -1 {
		t.Fatal("no delivery.email publish")
	}
	if sentIdx == -1 {
		t.Fatal("no notification.sent event published")
	}
	if sentIdx < deliveryIdx {
		t.Errorf("notification.sent published at %d, before the delivery message at %d", sentIdx, deliveryIdx)
	}
}

// Nothing was handed to a channel, so nothing was sent. Claiming otherwise would advance a
// notification to "sent" that no worker will ever pick up.
func TestHandleSend_NoNotificationSentWhenNothingDispatched(t *testing.T) {
	tests := []struct {
		name     string
		channels []string
		user     *models.User
		failOn   map[string]error
	}{
		{
			name:     "no channels on the request",
			channels: nil,
			user:     &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}},
		},
		{
			name:     "every channel dropped for want of a contact point",
			channels: []string{"email", "sms"},
			user:     &models.User{ID: "usr_1"},
		},
		{
			name:     "the only delivery publish fails",
			channels: []string{"email"},
			user:     &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}},
			failOn:   map[string]error{"delivery.email": errors.New("bus down")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bus := &fakeBus{failOn: tc.failOn}
			d := newTestDispatch(bus, &fakeNotifStore{}, tc.user)

			if err := d.handleSend(context.Background(), directSend(tc.channels...), firstAttempt()); err != nil {
				t.Fatalf("handleSend: %v", err)
			}

			names := bus.eventNames(t)
			if got := countEvent(names, "notification.sent"); got != 0 {
				t.Errorf("notification.sent count: got %d, want 0 (events: %v)", got, names)
			}
		})
	}
}

// A partial fan-out is still a hand-off: the channels that made it through are named, and the
// one that failed is not.
func TestHandleSend_NotificationSentNamesOnlyTheChannelsThatPublished(t *testing.T) {
	bus := &fakeBus{failOn: map[string]error{"delivery.email": errors.New("bus down")}}
	user := &models.User{ID: "usr_1", Contacts: map[string]string{"email": "a@example.com"}}
	d := newTestDispatch(bus, &fakeNotifStore{}, user)

	if err := d.handleSend(context.Background(), directSend("email", "inbox"), firstAttempt()); err != nil {
		t.Fatalf("handleSend: %v", err)
	}

	if got := bus.deliverySubjects(); len(got) != 1 || got[0] != "delivery.inbox" {
		t.Fatalf("delivery publishes: got %v, want [delivery.inbox]", got)
	}

	var sent *hermenats.EventMessage
	for _, e := range bus.events(t) {
		if e.Event == "notification.sent" {
			sent = &e
			break
		}
	}
	if sent == nil {
		t.Fatal("no notification.sent event published")
	}
	got, _ := sent.Metadata["channels"].([]any)
	if len(got) != 1 || got[0] != "inbox" {
		t.Errorf("metadata channels: got %v, want [inbox]", sent.Metadata["channels"])
	}
}

// Guard against the event name drifting: eventwriter keys the "sent" status off this exact
// string, and the two live in different packages with nothing but this constant between them.
func TestNotificationSentEventName(t *testing.T) {
	if eventNotificationSent != "notification.sent" {
		t.Fatalf("eventNotificationSent = %q; eventwriter.eventToStatus maps %q to StatusSent",
			eventNotificationSent, "notification.sent")
	}
}
