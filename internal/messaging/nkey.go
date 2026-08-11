// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// inboxPrefix returns the reply-inbox prefix for a named service, or "" when the service
// is unnamed (which leaves nats.go on its default _INBOX).
//
// This exists because a service's NATS identity has two halves. The seed says who you
// are; the inbox prefix says where your replies land. JetStream is request/reply
// underneath — publish acks and pulled messages both arrive on a subject the *client*
// chooses — so a user permitted to subscribe to `_INBOX.>` receives copies of every other
// service's pulled messages. That would route delivery.email payloads to a client with no
// permission on delivery.email at all, which is the entire subject scheme bypassed through
// the reply path. Confining each service to its own prefix is what lets the server-side
// subscribe permission be `_INBOX.<service>.>` instead.
func inboxPrefix(service string) string {
	if service == "" {
		return ""
	}
	return "_INBOX." + service
}

// WithIdentity authenticates this connection as the NKey user whose seed is in the file at
// seedPath, names the connection after the service, and confines its reply inboxes to
// `_INBOX.<service>`.
//
// The three go together deliberately: the NKey selects a user in
// deploy/k8s/base/infra/nats-accounts.conf, and that user's permissions are written
// against this service's subjects *and* this inbox prefix. Passing the seed without the
// name would leave the connection on the shared `_INBOX.>`, which no user is permitted to
// subscribe to.
//
// An empty seedPath is a deliberate no-op, the same opt-out WithCABundle has: `make
// infra-up` and the local overlay run NATS with no accounts, so HERMES_NATS_NKEY_SEED is
// unset there and the connection stays anonymous. It is not a silent downgrade — a server
// that defines accounts rejects an unauthenticated client, so the failure is a refused
// connection at startup, not a connection with fewer rights than intended.
//
// A seedPath that is set but unreadable or malformed fails Connect and names the file.
// nats.go re-reads the seed for every authentication challenge, so a rotated Secret is
// picked up on reconnect without a restart.
func WithIdentity(service, seedPath string) Option {
	return func(o *connectOptions) {
		if prefix := inboxPrefix(service); prefix != "" {
			o.nats = append(o.nats, nats.Name(service), nats.CustomInboxPrefix(prefix))
		}
		if seedPath == "" {
			return
		}
		opt, err := nats.NkeyOptionFromSeed(seedPath)
		if err != nil {
			o.errs = append(o.errs, fmt.Errorf("nats nkey seed %s: %w", seedPath, err))
			return
		}
		o.nats = append(o.nats, opt)
	}
}
