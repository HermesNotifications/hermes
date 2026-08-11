// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
