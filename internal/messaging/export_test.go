// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging

// InboxPrefixForTest exposes the reply-inbox prefix WithIdentity installs, so a test can
// assert it matches the `_INBOX.<service>.>` subscribe permission in nats-accounts.conf
// without reaching into a nats.Conn's private options.
func InboxPrefixForTest(service string) string { return inboxPrefix(service) }

// PollConsumersForTest runs one round of the stall monitor's CONSUMER.INFO poll against every
// consumer this client has subscribed, and reports whether each one could be inspected.
//
// It exists so the permissions tests can prove the grant on the wire. The monitor polls every 30s
// in production, which is far too slow for a test to wait out, and a poll that the server refuses
// is invisible in the probe by design: an unanswerable poll counts as no evidence, so a missing
// $JS.API.CONSUMER.INFO grant would silently switch stall detection off rather than fail anything.
func (c *Client) PollConsumersForTest() map[string]bool {
	c.mu.Lock()
	monitors := make([]*consumerProgress, len(c.progress))
	copy(monitors, c.progress)
	c.mu.Unlock()

	out := map[string]bool{}
	for _, p := range monitors {
		c.pollConsumer(p)
		_, _, unknown, _ := p.snapshot()
		out[p.consumer] = !unknown
	}
	return out
}

// SetMaxDeliveriesForTest lowers the delivery limit so integration tests
// don't sit through ten exponential-backoff retries. Returns a restore func.
func SetMaxDeliveriesForTest(n int) func() {
	old := maxDeliveries
	maxDeliveries = n
	return func() { maxDeliveries = old }
}
