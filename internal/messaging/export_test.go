// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

// SetMaxDeliveriesForTest lowers the delivery limit so integration tests
// don't sit through ten exponential-backoff retries. Returns a restore func.
func SetMaxDeliveriesForTest(n int) func() {
	old := maxDeliveries
	maxDeliveries = n
	return func() { maxDeliveries = old }
}
