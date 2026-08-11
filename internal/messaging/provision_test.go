// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

// ADR 0005 phase 4. Stream declaration moves to one provisioning identity, so services need a
// way to say "the streams I depend on must already exist" without being able to create them.
// EnsureStreams is that check, and these tests are about the property that makes the trade
// worth making: a missing stream is still a loud startup failure, not a service that comes up
// Ready and quietly fails every request.

// Every service that connects to the bus must declare which streams it depends on, or
// EnsureStreams silently checks nothing for it.
func TestStreamsForService_CoversEveryConnectingService(t *testing.T) {
	known := map[string]bool{}
	for _, s := range messaging.Streams {
		known[s.Name] = true
	}
	known[messaging.DLQStreamName] = true

	for _, service := range []string{
		"hermes-send",
		"hermes-dispatch",
		"hermes-worker-email",
		"hermes-worker-sms",
		"hermes-worker-inbox",
		"hermes-worker-events",
	} {
		streams, ok := messaging.StreamsForService[service]
		if !ok {
			t.Errorf("%s connects to the bus but declares no required streams", service)
			continue
		}
		if len(streams) == 0 {
			t.Errorf("%s declares an empty stream list", service)
		}
		for _, name := range streams {
			if !known[name] {
				t.Errorf("%s requires stream %q, which SetupStreams never creates", service, name)
			}
		}
	}
}

// The provisioner declares every stream any service requires. A stream some service waits for
// but nothing creates is a deployment that never converges.
func TestEveryRequiredStreamIsOneTheProvisionerCreates(t *testing.T) {
	created := map[string]bool{messaging.DLQStreamName: true}
	for _, s := range messaging.Streams {
		created[s.Name] = true
	}
	for service, streams := range messaging.StreamsForService {
		for _, name := range streams {
			if !created[name] {
				t.Errorf("%s requires %q but the provisioner does not create it", service, name)
			}
		}
	}
}

func TestEnsureStreams_PassesOnceTheProvisionerHasRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	provisioner := connectAs(t, messaging.ProvisionerService)
	if err := provisioner.SetupStreams(ctx, messaging.StreamOptions{}); err != nil {
		t.Fatalf("provisioner could not declare the streams: %v", err)
	}

	for service := range messaging.StreamsForService {
		client := connectAs(t, service)
		if err := client.EnsureStreams(ctx, service); err != nil {
			t.Errorf("%s: EnsureStreams after provisioning: %v", service, err)
		}
	}
}

// The failure this exists for, and the reason it names the stream: an operator reading
// "stream DELIVERY not found" knows the provisioning Job has not run. "nats: timeout" does not
// tell them that, which is what a bare Subscribe failure would have said.
func TestEnsureStreams_FailsAndNamesAStreamThatDoesNotExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := connectAs(t, "hermes-dispatch")
	err := client.EnsureStreams(ctx, "a-service-with-a-stream-nobody-creates")
	if err == nil {
		t.Fatal("EnsureStreams accepted a service it has no stream list for")
	}
	if !strings.Contains(err.Error(), "a-service-with-a-stream-nobody-creates") {
		t.Errorf("the error must name the unknown service, got: %v", err)
	}
}
