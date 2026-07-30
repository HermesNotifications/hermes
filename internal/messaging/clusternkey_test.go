// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package messaging_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

// ADR 0005 phase 3, verified against a real cluster rather than the embedded server in
// permissions_test.go. Same permissions file, but here it is loaded by the nats:2-alpine
// image from a Secret produced by the repository's own kustomize build, over TLS, with the
// public keys resolved from the StatefulSet's environment — every layer the deployment has
// and the embedded server does not.
//
//	kubectl -n <ns> get secret nats-server-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > /tmp/ca.crt
//	mkdir /tmp/seeds && for s in send dispatch worker-email worker-sms worker-inbox worker-events; do
//	  kubectl -n <ns> get secret nats-nkeys -o jsonpath="{.data.hermes-$s\.nk}" | base64 -d > /tmp/seeds/hermes-$s.nk
//	done
//	kubectl -n <ns> port-forward svc/nats 14222:4222 &
//	HERMES_TLS_NATS_ADDR=127.0.0.1:14222 HERMES_TLS_NATS_CA=/tmp/ca.crt \
//	  HERMES_TLS_NATS_SEED_DIR=/tmp/seeds \
//	  go test -tags=integration ./internal/messaging/ -run NKeyCluster -v
//
// As in clustertls_test.go, reaching the Service by port-forward means the TLS ServerName is
// "localhost", so the certificate under test needs a localhost SAN the committed manifest
// deliberately does not carry. That is the one deviation.

func nkeyClusterSeeds(t *testing.T) {
	t.Helper()
	if os.Getenv("HERMES_TLS_NATS_SEED_DIR") == "" {
		t.Skip("HERMES_TLS_NATS_SEED_DIR unset; needs an accounts-enabled NATS (see file header)")
	}
}

func connectClusterAs(t *testing.T, service string) *messaging.Client {
	t.Helper()
	client, err := messaging.Connect("tls://"+tlsClusterAddr(t),
		messaging.WithCABundle(tlsClusterCA(t)),
		tlsClusterIdentity(t, service))
	if err != nil {
		t.Fatalf("%s could not connect with its own credential: %v", service, err)
	}
	t.Cleanup(client.Close)
	return client
}

// The pipeline, hop by hop, each hop holding only its own service's credential.
func TestNKeyClusterPipeline(t *testing.T) {
	nkeyClusterSeeds(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	send := connectClusterAs(t, "hermes-send")
	if err := send.SetupStreams(ctx); err != nil {
		t.Fatalf("send could not declare the streams: %v", err)
	}
	if err := send.Publish(ctx, "notification.send", []byte(`{"nkey":"send"}`)); err != nil {
		t.Fatalf("send could not publish notification.send: %v", err)
	}

	dispatch := connectClusterAs(t, "hermes-dispatch")
	got := make(chan []byte, 4)
	if err := dispatch.Subscribe(messaging.SubscribeConfig{Subject: "notification.send", Consumer: "dispatch"},
		func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
			select {
			case got <- data:
			default:
			}
			return nil
		}); err != nil {
		t.Fatalf("dispatch could not consume notification.send: %v", err)
	}
	select {
	case <-got:
	case <-time.After(30 * time.Second):
		t.Fatal("dispatch received nothing from notification.send")
	}

	for _, subject := range []string{"delivery.email", "delivery.sms", "delivery.inbox", "notification.events", "dlq.notification.send"} {
		if err := dispatch.Publish(ctx, subject, []byte(`{"nkey":"dispatch"}`)); err != nil {
			t.Fatalf("dispatch could not publish %s: %v", subject, err)
		}
	}

	for _, w := range []struct{ service, channel, consumer string }{
		{"hermes-worker-email", "delivery.email", "worker-email"},
		{"hermes-worker-sms", "delivery.sms", "worker-sms"},
		{"hermes-worker-inbox", "delivery.inbox", "worker-inbox"},
	} {
		worker := connectClusterAs(t, w.service)
		delivered := make(chan []byte, 4)
		if err := worker.Subscribe(messaging.SubscribeConfig{Subject: w.channel, Consumer: w.consumer},
			func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
				select {
				case delivered <- data:
				default:
				}
				return nil
			}); err != nil {
			t.Fatalf("%s could not consume %s: %v", w.service, w.channel, err)
		}
		select {
		case <-delivered:
		case <-time.After(30 * time.Second):
			t.Fatalf("%s received nothing from %s", w.service, w.channel)
		}
		if err := worker.Publish(ctx, "notification.events", []byte(`{}`)); err != nil {
			t.Fatalf("%s could not report an event: %v", w.service, err)
		}
		if err := worker.Publish(ctx, "dlq."+w.channel, []byte(`{}`)); err != nil {
			t.Fatalf("%s could not dead-letter %s: %v", w.service, w.channel, err)
		}
	}

	events := connectClusterAs(t, "hermes-worker-events")
	seen := make(chan []byte, 8)
	if err := events.Subscribe(messaging.SubscribeConfig{Subject: "notification.events", Consumer: "event-writer"},
		func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
			select {
			case seen <- data:
			default:
			}
			return nil
		}); err != nil {
		t.Fatalf("event writer could not consume notification.events: %v", err)
	}
	select {
	case <-seen:
	case <-time.After(30 * time.Second):
		t.Fatal("event writer received nothing from notification.events")
	}
	if err := events.Publish(ctx, "dlq.notification.events", []byte(`{}`)); err != nil {
		t.Fatalf("event writer could not dead-letter notification.events: %v", err)
	}
}

// The denials that matter most, through the real client library against the real cluster.
// A publish refusal is asynchronous in NATS, so the observable for those is the JetStream
// ack that never arrives; the consumer-creation refusals surface as outright errors.
func TestNKeyClusterDenials(t *testing.T) {
	nkeyClusterSeeds(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("a delivery worker cannot inject deliveries", func(t *testing.T) {
		worker := connectClusterAs(t, "hermes-worker-email")
		pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := worker.Publish(pubCtx, "delivery.email", []byte(`{}`)); err == nil {
			t.Fatal("the email worker published to delivery.email")
		}
	})

	t.Run("dispatch cannot forge an ingestion", func(t *testing.T) {
		dispatch := connectClusterAs(t, "hermes-dispatch")
		pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := dispatch.Publish(pubCtx, "notification.send", []byte(`{}`)); err == nil {
			t.Fatal("dispatch published to notification.send")
		}
	})

	t.Run("dispatch cannot consume what it fanned out", func(t *testing.T) {
		dispatch := connectClusterAs(t, "hermes-dispatch")
		err := dispatch.Subscribe(messaging.SubscribeConfig{Subject: "delivery.email", Consumer: "dispatch-spy"},
			func(context.Context, []byte, messaging.DeliveryInfo) error { return nil })
		if err == nil {
			t.Fatal("dispatch created a consumer on delivery.email")
		}
		if !strings.Contains(err.Error(), "create consumer") {
			t.Errorf("expected a consumer-creation failure, got: %v", err)
		}
	})

	t.Run("one channel's worker cannot read another's", func(t *testing.T) {
		worker := connectClusterAs(t, "hermes-worker-email")
		err := worker.Subscribe(messaging.SubscribeConfig{Subject: "delivery.sms", Consumer: "worker-sms"},
			func(context.Context, []byte, messaging.DeliveryInfo) error { return nil })
		if err == nil {
			t.Fatal("the email worker created a consumer on delivery.sms")
		}
	})

	t.Run("the CA alone is not a credential", func(t *testing.T) {
		client, err := messaging.Connect("tls://"+tlsClusterAddr(t), messaging.WithCABundle(tlsClusterCA(t)))
		if err == nil {
			client.Close()
			t.Fatal("connected with the CA and no NKey — phase 2's gap is still open")
		}
		if !strings.Contains(err.Error(), "Authorization Violation") {
			t.Errorf("expected an authorization violation, got: %v", err)
		}
	})
}
