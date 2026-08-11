// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// ADR 0005 phase 3. These tests run a real nats-server configured from the repository's own
// deploy/k8s/base/infra/nats-accounts.conf, with a generated NKey for each service, and
// assert both halves of least privilege: that every service can do its job, and that it
// cannot do anyone else's.
//
// The permissions file is the artifact under test — not a copy of it — so a subject added
// to internal/messaging without a matching grant fails here rather than at 3am in
// production. That is the failure mode ADR 0005's Consequences section warns about.
//
// The server is embedded, so `make test` needs no infrastructure. A denial that was only
// reasoned about is a permission you do not have, which is why every entry in the tables
// below is asserted against the server's own -ERR response.

const accountsConfPath = "../../deploy/k8s/base/infra/nats-accounts.conf"

// natsRoles pairs each service's WithIdentity name with the configuration variable that
// carries its public key. Both halves are a contract with nats-accounts.conf: the name
// drives the `_INBOX.<service>.>` subscribe permission, the variable names the user.
var natsRoles = []struct{ service, envVar string }{
	{"hermes-send", "HERMES_NKEY_SEND"},
	// ADR 0005 phase 4. Not a service — cmd/natsprovision, run as a Job. It is in this table
	// because it is a bus identity with a credential and a permission set like any other.
	{messaging.ProvisionerService, "HERMES_NKEY_PROVISION"},
	{"hermes-dispatch", "HERMES_NKEY_DISPATCH"},
	{"hermes-worker-email", "HERMES_NKEY_WORKER_EMAIL"},
	{"hermes-worker-sms", "HERMES_NKEY_WORKER_SMS"},
	{"hermes-worker-inbox", "HERMES_NKEY_WORKER_INBOX"},
	{"hermes-worker-events", "HERMES_NKEY_WORKER_EVENTS"},
}

// --- the embedded server -------------------------------------------------------------

// ADR 0005 phase 4. Centrifugo's identity. It is a password user rather than an NKey user
// because centrifugo:v5 exposes no NKey setting in any form — verified against the image — and
// its only credential channel is the userinfo of nats_url.
const (
	centrifugoUser        = "centrifugo"
	centrifugoPasswordVar = "HERMES_CENTRIFUGO_NATS_PASSWORD"
)

type permFixture struct {
	url   string
	seeds map[string]string // service name → path to that service's NKey seed
	dir   string
	srv   *server.Server
	// centrifugoPassword is the value the server resolved $HERMES_CENTRIFUGO_NATS_PASSWORD
	// to, so the tests can present the same credential Centrifugo would.
	centrifugoPassword string
}

var permFixtureOnce = sync.OnceValues(startPermFixture)

// startPermFixture generates a keypair per role, exports the public keys the way the
// StatefulSet does (as environment variables the configuration resolves), and starts a
// server from the committed accounts file.
func startPermFixture() (*permFixture, error) {
	dir, err := os.MkdirTemp("", "hermes-nats-perms")
	if err != nil {
		return nil, err
	}
	f := &permFixture{seeds: map[string]string{}, dir: dir}

	for _, r := range natsRoles {
		kp, err := nkeys.CreateUser()
		if err != nil {
			return nil, err
		}
		pub, err := kp.PublicKey()
		if err != nil {
			return nil, err
		}
		seed, err := kp.Seed()
		if err != nil {
			return nil, err
		}
		seedPath := filepath.Join(dir, r.service+".nk")
		if err := os.WriteFile(seedPath, seed, 0o600); err != nil {
			return nil, err
		}
		f.seeds[r.service] = seedPath
		// The nats-server process reads these; in the cluster they come from the
		// nats-nkeys Secret. Set for the whole test binary rather than with t.Setenv
		// because the server parses its configuration once, before any subtest runs.
		if err := os.Setenv(r.envVar, pub); err != nil {
			return nil, err
		}
	}

	// ADR 0005 phase 4. Centrifugo's password reaches the server the same way the public keys
	// do — as a variable the configuration resolves — so an unset one is a parse error here
	// exactly as it is in the cluster. Generated per run so no test can depend on a constant —
	// but of the shape cmd/natskeys guarantees, because an unconstrained base64url draw fails
	// to parse about 2.3% of the time. See generateCentrifugoPassword.
	pw, err := generateCentrifugoPassword()
	if err != nil {
		return nil, err
	}
	f.centrifugoPassword = pw
	if err := os.Setenv(centrifugoPasswordVar, f.centrifugoPassword); err != nil {
		return nil, err
	}

	opts, err := serverOptsFromAccountsConf(dir)
	if err != nil {
		return nil, err
	}
	f.srv, err = server.NewServer(opts)
	if err != nil {
		return nil, err
	}
	go f.srv.Start()
	if !f.srv.ReadyForConnections(20 * time.Second) {
		return nil, fmt.Errorf("embedded nats server did not become ready")
	}
	f.url = f.srv.ClientURL()
	return f, nil
}

// serverOptsFromAccountsConf builds a minimal server configuration that `include`s the
// committed accounts file verbatim. Only the transport and store differ from the
// deployment: no TLS (tls_test.go and clustertls_test.go cover that) and an ephemeral
// store directory. The accounts block itself is byte-for-byte what the cluster loads.
func serverOptsFromAccountsConf(dir string) (*server.Options, error) {
	accounts, err := os.ReadFile(accountsConfPath)
	if err != nil {
		return nil, err
	}
	// nats-server resolves `include` relative to the including file, so the copy has to
	// sit next to the generated top-level configuration.
	if err := os.WriteFile(filepath.Join(dir, "accounts.conf"), accounts, 0o600); err != nil {
		return nil, err
	}
	top := fmt.Sprintf("port: -1\njetstream { store_dir: %q }\ninclude \"accounts.conf\"\n",
		filepath.Join(dir, "js"))
	confPath := filepath.Join(dir, "nats.conf")
	if err := os.WriteFile(confPath, []byte(top), 0o600); err != nil {
		return nil, err
	}
	opts, err := server.ProcessConfigFile(confPath)
	if err != nil {
		return nil, err
	}
	opts.NoLog, opts.NoSigs = true, true
	return opts, nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	// Only tear down what was actually started: most runs of this package never touch
	// the fixture, and starting a server just to shut it down would slow them down.
	if f, err, done := permFixtureIfStarted(); done {
		if err == nil && f != nil {
			f.srv.Shutdown()
			_ = os.RemoveAll(f.dir)
		}
	}
	os.Exit(code)
}

var permFixtureStarted bool

func permFixtureIfStarted() (*permFixture, error, bool) {
	if !permFixtureStarted {
		return nil, nil, false
	}
	f, err := permFixtureOnce()
	return f, err, true
}

func perms(t *testing.T) *permFixture {
	t.Helper()
	permFixtureStarted = true
	f, err := permFixtureOnce()
	if err != nil {
		t.Fatalf("start embedded nats with %s: %v", accountsConfPath, err)
	}
	return f
}

// connectAs dials as a service exactly the way its main.go does — through
// messaging.Connect and WithIdentity — so a grant proven here is a grant the production
// wiring has.
func connectAs(t *testing.T, service string) *messaging.Client {
	t.Helper()
	f := perms(t)
	seed, ok := f.seeds[service]
	if !ok {
		t.Fatalf("no seed generated for %q; is it in natsRoles?", service)
	}
	client, err := messaging.Connect(f.url, messaging.WithIdentity(service, seed))
	if err != nil {
		t.Fatalf("%s could not connect with its own credential: %v", service, err)
	}
	t.Cleanup(client.Close)
	return client
}

// rawConnAs dials with the same credential but no client library on top, and returns the
// channel the server's asynchronous -ERR responses arrive on. Permission violations are
// asynchronous in NATS — nc.Publish returns nil — so the server's own error text is the
// only trustworthy evidence that an operation was refused rather than quietly working.
func rawConnAs(t *testing.T, service string) (*nats.Conn, <-chan error) {
	t.Helper()
	f := perms(t)
	seed, ok := f.seeds[service]
	if !ok {
		t.Fatalf("no seed generated for %q; is it in natsRoles?", service)
	}
	nkeyOpt, err := nats.NkeyOptionFromSeed(seed)
	if err != nil {
		t.Fatalf("load seed for %s: %v", service, err)
	}
	errs := make(chan error, 8)
	nc, err := nats.Connect(f.url, nkeyOpt,
		nats.Name(service),
		nats.CustomInboxPrefix("_INBOX."+service),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			select {
			case errs <- err:
			default:
			}
		}))
	if err != nil {
		t.Fatalf("%s raw connect: %v", service, err)
	}
	t.Cleanup(nc.Close)
	return nc, errs
}

func awaitPermissionError(t *testing.T, errs <-chan error, what string) {
	t.Helper()
	select {
	case err := <-errs:
		if !strings.Contains(err.Error(), "Permissions Violation") {
			t.Errorf("%s: expected a permissions violation, got: %v", what, err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("%s: the server accepted it — no permissions violation arrived", what)
	}
}

// --- grants ---------------------------------------------------------------------------

// The whole pipeline, each hop authenticating as its own service. This is the grant half of
// the proof: if any user's permissions are too narrow, one of these steps fails.
func TestAccounts_PipelineRunsUnderPerServiceCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ADR 0005 phase 4. Provisioning is its own identity now, so the pipeline starts the way
	// the cluster does: the Job declares the streams, then the services run. Called twice on
	// purpose — the second call exercises the STREAM.UPDATE path against streams that already
	// exist, a different permission from STREAM.CREATE that a first-boot-only test misses.
	provisioner := connectAs(t, messaging.ProvisionerService)
	if err := provisioner.SetupStreams(ctx); err != nil {
		t.Fatalf("the provisioner could not declare the streams: %v", err)
	}
	if err := provisioner.SetupStreams(ctx); err != nil {
		t.Fatalf("the provisioner could not re-declare the streams: %v", err)
	}

	send := connectAs(t, "hermes-send")
	if err := send.EnsureStreams(ctx, "hermes-send"); err != nil {
		t.Fatalf("send could not verify its streams: %v", err)
	}
	if err := send.Publish(ctx, "notification.send", []byte(`{"hop":"send"}`)); err != nil {
		t.Fatalf("send could not publish notification.send: %v", err)
	}

	dispatch := connectAs(t, "hermes-dispatch")
	if err := dispatch.EnsureStreams(ctx, "hermes-dispatch"); err != nil {
		t.Fatalf("dispatch could not verify its streams: %v", err)
	}
	fromSend := subscribeOne(t, dispatch, "notification.send", "dispatch")
	if got := waitFor(t, fromSend, "dispatch consuming notification.send"); string(got) != `{"hop":"send"}` {
		t.Errorf("dispatch received %q", got)
	}
	for _, subject := range []string{"delivery.email", "delivery.sms", "delivery.inbox"} {
		if err := dispatch.Publish(ctx, subject, []byte(`{"hop":"dispatch"}`)); err != nil {
			t.Fatalf("dispatch could not fan out to %s: %v", subject, err)
		}
	}
	if err := dispatch.Publish(ctx, "notification.events", []byte(`{"hop":"dispatch"}`)); err != nil {
		t.Fatalf("dispatch could not publish an event: %v", err)
	}
	// internal/messaging/dlq.go publishes dead letters on behalf of any subscriber, so
	// every consuming service needs its own dlq.<its subject> right.
	if err := dispatch.Publish(ctx, "dlq.notification.send", []byte(`{}`)); err != nil {
		t.Fatalf("dispatch could not dead-letter its own subject: %v", err)
	}

	for _, w := range []struct{ service, channel, consumer string }{
		{"hermes-worker-email", "delivery.email", "worker-email"},
		{"hermes-worker-sms", "delivery.sms", "worker-sms"},
		{"hermes-worker-inbox", "delivery.inbox", "worker-inbox"},
	} {
		worker := connectAs(t, w.service)
		if err := worker.EnsureStreams(ctx, w.service); err != nil {
			t.Fatalf("%s could not verify its streams: %v", w.service, err)
		}
		delivered := subscribeOne(t, worker, w.channel, w.consumer)
		waitFor(t, delivered, w.service+" consuming "+w.channel)
		if err := worker.Publish(ctx, "notification.events", []byte(`{"hop":"`+w.consumer+`"}`)); err != nil {
			t.Fatalf("%s could not report an event: %v", w.service, err)
		}
		if err := worker.Publish(ctx, "dlq."+w.channel, []byte(`{}`)); err != nil {
			t.Fatalf("%s could not dead-letter %s: %v", w.service, w.channel, err)
		}
	}

	events := connectAs(t, "hermes-worker-events")
	if err := events.EnsureStreams(ctx, "hermes-worker-events"); err != nil {
		t.Fatalf("worker-events could not verify its streams: %v", err)
	}
	seen := subscribeOne(t, events, "notification.events", "event-writer")
	waitFor(t, seen, "event writer consuming notification.events")
	if err := events.Publish(ctx, "dlq.notification.events", []byte(`{}`)); err != nil {
		t.Fatalf("worker-events could not dead-letter notification.events: %v", err)
	}
}

func subscribeOne(t *testing.T, c *messaging.Client, subject, consumer string) <-chan []byte {
	t.Helper()
	out := make(chan []byte, 8)
	err := c.Subscribe(messaging.SubscribeConfig{Subject: subject, Consumer: consumer},
		func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
			select {
			case out <- data:
			default:
			}
			return nil
		})
	if err != nil {
		t.Fatalf("subscribe %s as %s: %v", subject, consumer, err)
	}
	return out
}

func waitFor(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	select {
	case data := <-ch:
		return data
	case <-time.After(20 * time.Second):
		t.Fatalf("%s: nothing delivered", what)
		return nil
	}
}

// --- denials --------------------------------------------------------------------------

// Publishing is where forgery and injection live: a worker that can publish delivery.*
// can deliver to anyone, and a service that can publish notification.send can invent
// notifications. Each case here is refused by the server, observed, not assumed.
func TestAccounts_DeniedPublishes(t *testing.T) {
	cases := []struct{ name, service, subject string }{
		// A delivery worker must not be able to inject deliveries — not into another
		// channel and not even into its own.
		{"email worker cannot publish its own delivery subject", "hermes-worker-email", "delivery.email"},
		{"email worker cannot publish another channel's", "hermes-worker-email", "delivery.sms"},
		{"sms worker cannot publish its own delivery subject", "hermes-worker-sms", "delivery.sms"},
		{"inbox worker cannot publish its own delivery subject", "hermes-worker-inbox", "delivery.inbox"},

		// Dispatch fans out; it must not be able to fabricate an ingestion.
		{"dispatch cannot publish notification.send", "hermes-dispatch", "notification.send"},

		// The event writer only reads events. Manufacturing one would let it drive a
		// notification's status rollup.
		{"event writer cannot publish notification.events", "hermes-worker-events", "notification.events"},

		// Send is publish-only, on exactly one subject.
		{"send cannot publish a delivery subject", "hermes-send", "delivery.email"},
		{"send cannot publish an event", "hermes-send", "notification.events"},
		{"send cannot dead-letter", "hermes-send", "dlq.notification.send"},

		// Dead letters are scoped to the subject a service actually consumes, so a
		// compromised worker cannot forge another's failure record.
		{"email worker cannot dead-letter the sms subject", "hermes-worker-email", "dlq.delivery.sms"},
		{"dispatch cannot dead-letter a delivery subject", "hermes-dispatch", "dlq.delivery.email"},
		{"nobody can dead-letter over a wildcard", "hermes-dispatch", "dlq.everything"},

		// JetStream authorisation is a separate permission surface from the subjects.
		// Dispatch must not be able to consume what it fanned out.
		{"dispatch cannot create a DELIVERY consumer", "hermes-dispatch", "$JS.API.CONSUMER.CREATE.DELIVERY.spy.delivery.email"},
		{"dispatch cannot fetch from the email worker's consumer", "hermes-dispatch", "$JS.API.CONSUMER.MSG.NEXT.DELIVERY.worker-email"},

		// One channel's worker must not read another channel's recipients.
		{"email worker cannot create an sms consumer", "hermes-worker-email", "$JS.API.CONSUMER.CREATE.DELIVERY.worker-sms.delivery.sms"},
		{"email worker cannot fetch from the sms consumer", "hermes-worker-email", "$JS.API.CONSUMER.MSG.NEXT.DELIVERY.worker-sms"},
		{"email worker cannot consume the ingestion stream", "hermes-worker-email", "$JS.API.CONSUMER.CREATE.NOTIFICATIONS.spy.notification.send"},
		{"email worker cannot ack the sms consumer", "hermes-worker-email", "$JS.ACK.DELIVERY.worker-sms.1.1.1.1.0"},

		// Send has no consumer at all.
		{"send cannot create any consumer", "hermes-send", "$JS.API.CONSUMER.CREATE.NOTIFICATIONS.spy.notification.send"},
		{"send cannot fetch from the dispatch consumer", "hermes-send", "$JS.API.CONSUMER.MSG.NEXT.NOTIFICATIONS.dispatch"},

		// Nobody gets the destructive or enumerating halves of the JetStream API. These
		// are denied by omission from an allow list, which is exactly the kind of
		// negative space worth an assertion.
		{"nobody can delete a stream", "hermes-dispatch", "$JS.API.STREAM.DELETE.DELIVERY"},
		{"nobody can purge a stream", "hermes-dispatch", "$JS.API.STREAM.PURGE.DELIVERY"},
		{"nobody can delete a consumer", "hermes-dispatch", "$JS.API.CONSUMER.DELETE.NOTIFICATIONS.dispatch"},
		{"nobody can read a stream message directly", "hermes-dispatch", "$JS.API.DIRECT.GET.DELIVERY"},
		{"nobody can read a stream message by request", "hermes-dispatch", "$JS.API.STREAM.MSG.GET.DELIVERY"},
		{"nobody can list streams", "hermes-dispatch", "$JS.API.STREAM.LIST"},
		{"nobody can read account info", "hermes-dispatch", "$JS.API.INFO"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nc, errs := rawConnAs(t, tc.service)
			if err := nc.Publish(tc.subject, []byte("x")); err != nil {
				t.Fatalf("publish returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, tc.service+" publishing "+tc.subject)
		})
	}
}

// Subscribing is where eavesdropping lives. The subtle cases are the last three: a pull
// consumer's messages arrive on an inbox, so the inbox is the subject that actually
// carries recipient addresses and rendered bodies.
func TestAccounts_DeniedSubscriptions(t *testing.T) {
	cases := []struct{ name, service, subject string }{
		{"email worker cannot subscribe its own delivery subject", "hermes-worker-email", "delivery.email"},
		{"dispatch cannot subscribe a delivery subject", "hermes-dispatch", "delivery.email"},
		{"dispatch cannot subscribe the delivery wildcard", "hermes-dispatch", "delivery.*"},
		{"send cannot subscribe the subject it publishes", "hermes-send", "notification.send"},
		{"event writer cannot subscribe notification.events", "hermes-worker-events", "notification.events"},
		{"nobody can subscribe everything", "hermes-dispatch", ">"},
		{"nobody can subscribe the JetStream API", "hermes-dispatch", "$JS.>"},

		// The reply path. WithIdentity confines each service to _INBOX.<service>; without
		// these denials a compromised worker could receive copies of every other
		// service's pulled messages and read delivery.* without any delivery permission.
		{"nobody can subscribe the shared inbox space", "hermes-worker-email", "_INBOX.>"},
		{"one service cannot subscribe another's inbox", "hermes-worker-email", "_INBOX.hermes-dispatch.>"},
		{"one service cannot subscribe another's inbox by wildcard", "hermes-worker-email", "_INBOX.*.>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nc, errs := rawConnAs(t, tc.service)
			if _, err := nc.SubscribeSync(tc.subject); err != nil {
				t.Fatalf("subscribe returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, tc.service+" subscribing "+tc.subject)
		})
	}
}

// The denial as the application actually meets it. The table above proves the server
// refuses the wire operation; this proves internal/messaging surfaces that refusal as a
// startup error rather than a worker that silently consumes nothing.
func TestAccounts_CrossChannelSubscribeFailsThroughTheClient(t *testing.T) {
	worker := connectAs(t, "hermes-worker-email")

	err := worker.Subscribe(messaging.SubscribeConfig{Subject: "delivery.sms", Consumer: "worker-sms"},
		func(context.Context, []byte, messaging.DeliveryInfo) error { return nil })
	if err == nil {
		t.Fatal("the email worker created a consumer on delivery.sms")
	}
	if !strings.Contains(err.Error(), "create consumer") {
		t.Errorf("expected a consumer-creation failure, got: %v", err)
	}
}

// With an account defined there is no anonymous fallback. This is the property that makes
// the whole file worth anything: phase 2 left the bus open to anyone holding the CA.
func TestAccounts_UnauthenticatedConnectionIsRejected(t *testing.T) {
	f := perms(t)

	client, err := messaging.Connect(f.url)
	if err == nil {
		client.Close()
		t.Fatal("connected with no credential at all")
	}
	if !strings.Contains(err.Error(), "Authorization Violation") {
		t.Errorf("expected an authorization violation, got: %v", err)
	}
}

// A valid NKey that is not one of the six is still nobody.
func TestAccounts_UnknownNKeyIsRejected(t *testing.T) {
	f := perms(t)

	kp, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "stranger.nk")
	writeFile(t, path, string(seed))

	client, err := messaging.Connect(f.url, messaging.WithIdentity("hermes-dispatch", path))
	if err == nil {
		client.Close()
		t.Fatal("a keypair the configuration never named was accepted")
	}
	if !strings.Contains(err.Error(), "Authorization Violation") {
		t.Errorf("expected an authorization violation, got: %v", err)
	}
}

// Provisioning fails closed. The public keys come from the environment, so a cluster where
// the nats-nkeys Secret did not land must refuse to start rather than start with an
// account nobody can use — or worse, with the users block silently empty.
func TestAccounts_MissingKeyVariableRefusesToStart(t *testing.T) {
	perms(t) // ensure the variables are populated first, then remove one

	const v = "HERMES_NKEY_WORKER_SMS"
	saved := os.Getenv(v)
	t.Cleanup(func() { _ = os.Setenv(v, saved) })
	if err := os.Unsetenv(v); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	_, err := serverOptsFromAccountsConf(t.TempDir())
	if err == nil {
		t.Fatal("the configuration parsed with an unset NKey variable")
	}
	if !strings.Contains(err.Error(), v) {
		t.Errorf("the error must name the missing variable %q, got: %v", v, err)
	}
}

// --- stream provisioning (ADR 0005 phase 4) -------------------------------------------

// The grant this whole phase exists to isolate. Only the provisioner may declare streams, and
// the denial for everyone else is asserted per service and per stream rather than sampled —
// this is the permission phase 3 named as its residual over-grant.
func TestProvisioning_NoServiceCanCreateOrUpdateAnyStream(t *testing.T) {
	streams := []string{"NOTIFICATIONS", "DELIVERY", "EVENTS", messaging.DLQStreamName}

	for _, r := range natsRoles {
		if r.service == messaging.ProvisionerService {
			continue
		}
		for _, stream := range streams {
			for _, verb := range []string{"CREATE", "UPDATE"} {
				subject := "$JS.API.STREAM." + verb + "." + stream
				t.Run(r.service+" "+subject, func(t *testing.T) {
					nc, errs := rawConnAs(t, r.service)
					if err := nc.Publish(subject, []byte("{}")); err != nil {
						t.Fatalf("publish returned an error before the server saw it: %v", err)
					}
					if err := nc.Flush(); err != nil {
						t.Fatalf("flush: %v", err)
					}
					awaitPermissionError(t, errs, r.service+" publishing "+subject)
				})
			}
		}
	}
}

// SetupStreams through the client, as a service would call it. The table above proves the wire
// operation is refused; this proves internal/messaging surfaces the refusal as an error, which
// is what makes bootstrap exit non-zero rather than log and continue.
func TestProvisioning_SetupStreamsFailsForAServiceThroughTheClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := connectAs(t, "hermes-dispatch").SetupStreams(ctx); err == nil {
		t.Fatal("dispatch declared the streams")
	}
}

// The provisioner is not a back door. It can shape streams and do nothing else: a leaked
// provisioning seed must not become a way to forge notifications or read recipients.
func TestProvisioning_ProvisionerCannotTouchTraffic(t *testing.T) {
	publishes := []string{
		"notification.send",
		"notification.events",
		"delivery.email",
		"delivery.sms",
		"delivery.inbox",
		"dlq.notification.send",
		"centrifugo.client.user#usr_abc123",
		// Consuming is how it would read traffic, and it has no consumer rights at all.
		"$JS.API.CONSUMER.CREATE.DELIVERY.spy.delivery.email",
		"$JS.API.CONSUMER.MSG.NEXT.DELIVERY.worker-email",
		// Nor the destructive half. Provisioning means create and update, not delete.
		"$JS.API.STREAM.DELETE.DELIVERY",
		"$JS.API.STREAM.PURGE.DELIVERY",
		"$JS.API.STREAM.LIST",
		"$JS.API.DIRECT.GET.DELIVERY",
		"$JS.API.STREAM.MSG.GET.DELIVERY",
	}
	for _, subject := range publishes {
		t.Run("publish "+subject, func(t *testing.T) {
			nc, errs := rawConnAs(t, messaging.ProvisionerService)
			if err := nc.Publish(subject, []byte("{}")); err != nil {
				t.Fatalf("publish returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, "provisioner publishing "+subject)
		})
	}

	for _, subject := range []string{"delivery.email", "notification.send", ">", "_INBOX.>"} {
		t.Run("subscribe "+subject, func(t *testing.T) {
			nc, errs := rawConnAs(t, messaging.ProvisionerService)
			if _, err := nc.SubscribeSync(subject); err != nil {
				t.Fatalf("subscribe returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, "provisioner subscribing "+subject)
		})
	}
}

// STREAM.INFO replaces CREATE/UPDATE for services, so it has to be narrow too: a service may
// read the configuration of the streams it depends on and no others. Derived from
// StreamsForService so the assertion cannot drift from the map the code uses.
func TestProvisioning_ServicesCannotReadStreamsTheyDoNotDependOn(t *testing.T) {
	all := []string{"NOTIFICATIONS", "DELIVERY", "EVENTS", messaging.DLQStreamName}

	for service, required := range messaging.StreamsForService {
		needs := map[string]bool{}
		for _, name := range required {
			needs[name] = true
		}
		for _, stream := range all {
			if needs[stream] {
				continue
			}
			subject := "$JS.API.STREAM.INFO." + stream
			t.Run(service+" "+subject, func(t *testing.T) {
				nc, errs := rawConnAs(t, service)
				if err := nc.Publish(subject, []byte("{}")); err != nil {
					t.Fatalf("publish returned an error before the server saw it: %v", err)
				}
				if err := nc.Flush(); err != nil {
					t.Fatalf("flush: %v", err)
				}
				awaitPermissionError(t, errs, service+" publishing "+subject)
			})
		}
	}
}

// --- centrifugo (ADR 0005 phase 4) ----------------------------------------------------

// rawConnAsCentrifugo dials the way centrifugo:v5 does: a password in the URL's userinfo, no
// NKey, no client certificate. That is not a simplification for the test's convenience — it is
// the only thing the image can present.
func rawConnAsCentrifugo(t *testing.T) (*nats.Conn, <-chan error) {
	t.Helper()
	f := perms(t)
	errs := make(chan error, 8)
	nc, err := nats.Connect(f.url,
		nats.UserInfo(centrifugoUser, f.centrifugoPassword),
		nats.Name(centrifugoUser),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			select {
			case errs <- err:
			default:
			}
		}))
	if err != nil {
		t.Fatalf("centrifugo could not connect with its password: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc, errs
}

// The grant half. These three subjects are what a nats-server trace of a real two-node
// Centrifugo fan-out actually showed, and all three are needed in both directions — a node
// publishes to ANOTHER node's centrifugo.node.<id> subject, which a publish grant limited to
// control and client would refuse.
func TestCentrifugo_CanUseItsOwnSubjectsInBothDirections(t *testing.T) {
	subjects := []string{
		"centrifugo.control",
		"centrifugo.node.04749b5f-6464-4217-838d-a59cd7403163",
		"centrifugo.client.user#usr_abc123",
	}

	for _, subject := range subjects {
		t.Run(subject, func(t *testing.T) {
			nc, errs := rawConnAsCentrifugo(t)
			sub, err := nc.SubscribeSync(subject)
			if err != nil {
				t.Fatalf("subscribe %s: %v", subject, err)
			}
			if err := nc.Publish(subject, []byte("x")); err != nil {
				t.Fatalf("publish %s: %v", subject, err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			if _, err := sub.NextMsg(5 * time.Second); err != nil {
				t.Errorf("%s: nothing round-tripped: %v", subject, err)
			}
			select {
			case err := <-errs:
				t.Errorf("%s: server refused an operation Centrifugo needs: %v", subject, err)
			default:
			}
		})
	}
}

// The denial half, and the point of giving Centrifugo its own user at all: it is a third-party
// process reachable from the public internet, and a compromise of it must not reach the
// notification pipeline. Every case is refused by the server, observed.
func TestCentrifugo_CannotTouchThePipeline(t *testing.T) {
	publishes := []string{
		"notification.send",
		"notification.events",
		"delivery.email",
		"delivery.sms",
		"delivery.inbox",
		"dlq.delivery.email",
		"$JS.API.STREAM.CREATE.DELIVERY",
		"$JS.API.CONSUMER.CREATE.DELIVERY.spy.delivery.email",
		"$JS.API.CONSUMER.MSG.NEXT.DELIVERY.worker-email",
	}
	for _, subject := range publishes {
		t.Run("publish "+subject, func(t *testing.T) {
			nc, errs := rawConnAsCentrifugo(t)
			if err := nc.Publish(subject, []byte("x")); err != nil {
				t.Fatalf("publish returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, "centrifugo publishing "+subject)
		})
	}

	subscribes := []string{
		"delivery.email",
		"delivery.*",
		"notification.send",
		"notification.events",
		">",
		"$JS.>",
		// The inbox space is where pulled messages actually arrive, so this is the
		// subscription that would hand Centrifugo every recipient address in flight.
		"_INBOX.>",
		"_INBOX.hermes-dispatch.>",
	}
	for _, subject := range subscribes {
		t.Run("subscribe "+subject, func(t *testing.T) {
			nc, errs := rawConnAsCentrifugo(t)
			if _, err := nc.SubscribeSync(subject); err != nil {
				t.Fatalf("subscribe returned an error before the server saw it: %v", err)
			}
			if err := nc.Flush(); err != nil {
				t.Fatalf("flush: %v", err)
			}
			awaitPermissionError(t, errs, "centrifugo subscribing "+subject)
		})
	}
}

// And the reverse direction, which is the half that is easy to forget: no Hermes service may
// reach into Centrifugo's subject space either. A worker that could publish
// centrifugo.client.<channel> could push arbitrary payloads to any user's browser, bypassing
// the inbox worker and the store entirely.
func TestCentrifugo_SubjectSpaceIsClosedToHermesServices(t *testing.T) {
	subjects := []string{
		"centrifugo.control",
		"centrifugo.client.user#usr_abc123",
		"centrifugo.>",
	}
	for _, service := range []string{"hermes-dispatch", "hermes-worker-inbox", "hermes-send"} {
		for _, subject := range subjects {
			t.Run(service+" publish "+subject, func(t *testing.T) {
				nc, errs := rawConnAs(t, service)
				if err := nc.Publish(subject, []byte("x")); err != nil {
					t.Fatalf("publish returned an error before the server saw it: %v", err)
				}
				if err := nc.Flush(); err != nil {
					t.Fatalf("flush: %v", err)
				}
				awaitPermissionError(t, errs, service+" publishing "+subject)
			})
			t.Run(service+" subscribe "+subject, func(t *testing.T) {
				nc, errs := rawConnAs(t, service)
				if _, err := nc.SubscribeSync(subject); err != nil {
					t.Fatalf("subscribe returned an error before the server saw it: %v", err)
				}
				if err := nc.Flush(); err != nil {
					t.Fatalf("flush: %v", err)
				}
				awaitPermissionError(t, errs, service+" subscribing "+subject)
			})
		}
	}
}

// A wrong password is nobody, and so is no password. Verified rather than assumed because it is
// the property that makes the Centrifugo user worth having: phase 3 left the base ConfigMap
// pointing at a plaintext URL, and this is what that would now meet.
func TestCentrifugo_WrongOrMissingPasswordIsRejected(t *testing.T) {
	f := perms(t)

	for _, tc := range []struct {
		name string
		opts []nats.Option
	}{
		{"wrong password", []nats.Option{nats.UserInfo(centrifugoUser, "not-the-password")}},
		{"no credential at all", nil},
		{"right password, wrong user", []nats.Option{nats.UserInfo("centrifuge", f.centrifugoPassword)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc, err := nats.Connect(f.url, tc.opts...)
			if err == nil {
				nc.Close()
				t.Fatal("the server accepted the connection")
			}
			if !strings.Contains(err.Error(), "Authorization Violation") {
				t.Errorf("expected an authorization violation, got: %v", err)
			}
		})
	}
}

// --- drift guard ----------------------------------------------------------------------

// ADR 0005's Consequences section: "a service that gains a publish target and does not
// gain the permission fails at runtime, not at deploy". This is the deploy-time half —
// every subject the code knows about has to appear in the permissions file, and every
// service's inbox prefix has to match what WithIdentity installs.
func TestAccounts_ConfCoversEverySubjectTheCodeUses(t *testing.T) {
	raw, err := os.ReadFile(accountsConfPath)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConfPath, err)
	}
	conf := string(raw)

	for _, stream := range messaging.Streams {
		for _, subject := range stream.Subjects {
			if !strings.Contains(conf, `"`+subject+`"`) {
				t.Errorf("subject %q is in messaging.Streams but has no permission in %s",
					subject, accountsConfPath)
			}
		}
		for _, verb := range []string{"CREATE", "UPDATE"} {
			api := "$JS.API.STREAM." + verb + "." + stream.Name
			if !strings.Contains(conf, `"`+api+`"`) {
				t.Errorf("stream %q needs %q; SetupStreams cannot declare it otherwise",
					stream.Name, api)
			}
		}
	}

	for _, r := range natsRoles {
		want := `"` + messaging.InboxPrefixForTest(r.service) + `.>"`
		if !strings.Contains(conf, want) {
			t.Errorf("%s connects with inbox prefix %s but %s grants no matching subscribe",
				r.service, want, accountsConfPath)
		}
		if !strings.Contains(conf, "$"+r.envVar) {
			t.Errorf("%s has no user in %s (expected nkey: $%s)", r.service, accountsConfPath, r.envVar)
		}
	}
}

// userBlocks splits the permissions file into one text block per user, keyed by the $VARIABLE
// naming that user. Crude but sufficient: the file lists users in a flat array and each block
// starts at its own credential line, so "which user holds this grant" is answerable without a
// HOCON parser — and answering it is the only way to assert that a grant belongs to exactly one
// user rather than merely appearing somewhere in the file.
func userBlocks(t *testing.T, conf string) map[string]string {
	t.Helper()
	const marker = "nkey: $"
	blocks := map[string]string{}
	idx := strings.Index(conf, marker)
	for idx >= 0 {
		rest := conf[idx+len(marker):]
		name, _, _ := strings.Cut(rest, "\n")
		next := strings.Index(rest, marker)
		body := rest
		if next >= 0 {
			body = rest[:next]
		}
		blocks[strings.TrimSpace(name)] = body
		if next < 0 {
			break
		}
		idx = idx + len(marker) + next
	}
	if len(blocks) != len(natsRoles) {
		t.Fatalf("parsed %d user blocks for %d roles; the parser or the file shape changed",
			len(blocks), len(natsRoles))
	}
	return blocks
}

// ADR 0005 phase 4's central claim, checked at deploy time rather than only on the wire: stream
// creation belongs to one identity. The in-cluster tests above prove the server refuses it for
// everyone else; this proves the file cannot quietly grow the grant back for a service.
func TestAccounts_OnlyTheProvisionerMayDeclareStreams(t *testing.T) {
	raw, err := os.ReadFile(accountsConfPath)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConfPath, err)
	}
	blocks := userBlocks(t, string(raw))

	provisionerVar := ""
	for _, r := range natsRoles {
		if r.service == messaging.ProvisionerService {
			provisionerVar = r.envVar
		}
	}
	if provisionerVar == "" {
		t.Fatalf("%s is not in natsRoles", messaging.ProvisionerService)
	}

	streams := []string{"NOTIFICATIONS", "DELIVERY", "EVENTS", messaging.DLQStreamName}
	for envVar, body := range blocks {
		for _, stream := range streams {
			for _, verb := range []string{"CREATE", "UPDATE"} {
				grant := `"$JS.API.STREAM.` + verb + "." + stream + `"`
				has := strings.Contains(body, grant)
				if envVar == provisionerVar && !has {
					t.Errorf("the provisioner is missing %s; it cannot declare %s without it",
						grant, stream)
				}
				if envVar != provisionerVar && has {
					t.Errorf("$%s holds %s — stream declaration must belong to the provisioner alone",
						envVar, grant)
				}
			}
		}
	}
}

// STREAM.INFO is what services got in exchange, so it has to match messaging.StreamsForService
// exactly in both directions: a stream a service waits for but cannot read makes EnsureStreams
// fail forever, and a grant with no entry in the map is an over-grant nothing needs.
func TestAccounts_StreamInfoGrantsMatchStreamsForService(t *testing.T) {
	raw, err := os.ReadFile(accountsConfPath)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConfPath, err)
	}
	blocks := userBlocks(t, string(raw))

	byService := map[string]string{}
	for _, r := range natsRoles {
		byService[r.service] = r.envVar
	}

	allStreams := []string{"NOTIFICATIONS", "DELIVERY", "EVENTS", messaging.DLQStreamName}
	for service, required := range messaging.StreamsForService {
		envVar, ok := byService[service]
		if !ok {
			t.Errorf("%s is in StreamsForService but has no user in natsRoles", service)
			continue
		}
		body, ok := blocks[envVar]
		if !ok {
			t.Errorf("$%s has no user block in %s", envVar, accountsConfPath)
			continue
		}
		needs := map[string]bool{}
		for _, name := range required {
			needs[name] = true
		}
		for _, stream := range allStreams {
			grant := `"$JS.API.STREAM.INFO.` + stream + `"`
			has := strings.Contains(body, grant)
			switch {
			case needs[stream] && !has:
				t.Errorf("%s requires stream %s but $%s is not granted %s, so EnsureStreams cannot pass",
					service, stream, envVar, grant)
			case !needs[stream] && has:
				t.Errorf("$%s holds %s but %s does not require stream %s", envVar, grant, service, stream)
			}
		}
	}
}
