// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package provider

// Manifest is the registration record for a delivery provider. Phase 1 carries
// only identity + channel; capabilities, cost tier, ingress style, and derived
// NATS subjects arrive in later phases (see the provider-plugin design doc).
type Manifest struct {
	ID      string // provider id, e.g. "ses"
	Channel string // the channel slug this provider serves
}
