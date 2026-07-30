// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

// InboxPrefixForTest exposes the reply-inbox prefix WithIdentity installs, so a test can
// assert it matches the `_INBOX.<service>.>` subscribe permission in nats-accounts.conf
// without reaching into a nats.Conn's private options.
func InboxPrefixForTest(service string) string { return inboxPrefix(service) }

// SetMaxDeliveriesForTest lowers the delivery limit so integration tests
// don't sit through ten exponential-backoff retries. Returns a restore func.
func SetMaxDeliveriesForTest(n int) func() {
	old := maxDeliveries
	maxDeliveries = n
	return func() { maxDeliveries = old }
}
