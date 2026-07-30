// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

import (
	"context"
	"fmt"
)

// ADR 0005 phase 4. Stream declaration belongs to one identity, not to all six services.
//
// Phase 3 gave every service `$JS.API.STREAM.CREATE.<S>` and `…UPDATE.<S>` for all four
// streams, because every service called SetupStreams at boot. That grant is what let a
// compromised worker rewrite a stream's configuration — MaxAge down to a second discards
// every message in flight, and removing a subject makes publishes to it fail — on streams it
// neither publishes to nor consumes.
//
// The reason phase 3 accepted it was that self-declaration gives a startup-ordering guarantee:
// any service can heal a missing stream, and MustSetupStreams exits non-zero, so a service
// never runs against a bus that is not ready. EnsureStreams keeps that guarantee without the
// grant. A service now *verifies* the streams it depends on and refuses to start if they are
// absent, which needs only `$JS.API.STREAM.INFO.<S>` — a read. What is given up is
// self-healing, and streams are provisioned the same way the database schema already is: by a
// Job that runs before the services, with a crash-loop as the convergence mechanism.

// DLQStreamName is the dead-letter stream. Exported so provisioning tooling and the drift
// guards in the test suite can name the same stream the DLQ machinery writes to.
const DLQStreamName = dlqStreamName

// ProvisionerService is the identity that declares streams, and the only one holding
// STREAM.CREATE and STREAM.UPDATE. It is not a service: cmd/natsprovision runs as a Job,
// declares the streams and exits.
const ProvisionerService = "hermes-natsprovision"

// StreamsForService lists the streams each service refuses to start without. It is a per-service
// list rather than "all of them" so the matching `$JS.API.STREAM.INFO.<S>` grants stay narrow —
// a service that never touches DELIVERY should not be able to read its configuration either.
//
// This map is a contract with deploy/k8s/base/infra/nats-accounts.conf, checked in both
// directions by TestAccounts_StreamInfoGrantsMatchStreamsForService: an entry here without a
// grant is a service that cannot boot, and a grant without an entry here is an over-grant.
var StreamsForService = map[string][]string{
	// Publishes notification.send. Nothing else, so nothing else has to exist for it to run.
	"hermes-send": {"NOTIFICATIONS"},
	// Consumes NOTIFICATIONS, fans out to DELIVERY, reports on EVENTS, dead-letters to DLQ.
	"hermes-dispatch": {"NOTIFICATIONS", "DELIVERY", "EVENTS", DLQStreamName},
	// The delivery workers consume DELIVERY, report on EVENTS and dead-letter to DLQ. DLQ is
	// listed even though it is only written on failure: discovering it is missing at the
	// moment a message needs preserving is the worst possible time.
	"hermes-worker-email":  {"DELIVERY", "EVENTS", DLQStreamName},
	"hermes-worker-sms":    {"DELIVERY", "EVENTS", DLQStreamName},
	"hermes-worker-inbox":  {"DELIVERY", "EVENTS", DLQStreamName},
	"hermes-worker-events": {"EVENTS", DLQStreamName},
}

// StreamNames lists every stream SetupStreams declares, DLQ included. It exists so
// cmd/natsprovision can report what it provisioned without duplicating the list.
func StreamNames() []string {
	names := make([]string, 0, len(Streams)+1)
	for _, s := range Streams {
		names = append(names, s.Name)
	}
	return append(names, DLQStreamName)
}

// EnsureStreams verifies that every stream the named service depends on already exists, and
// returns an error naming the first one that does not.
//
// It deliberately cannot create anything. Under phase 4's permissions the calling service holds
// only `$JS.API.STREAM.INFO.<S>` for the streams in its own StreamsForService entry, so a
// service attempting to widen this into a create would be refused by the server as well.
func (c *Client) EnsureStreams(ctx context.Context, service string) error {
	required, ok := StreamsForService[service]
	if !ok {
		return fmt.Errorf("no required streams declared for service %q: add it to messaging.StreamsForService", service)
	}
	for _, name := range required {
		if _, err := c.js.Stream(ctx, name); err != nil {
			return fmt.Errorf("stream %s is not available to %s (has cmd/natsprovision run?): %w", name, service, err)
		}
	}
	return nil
}
