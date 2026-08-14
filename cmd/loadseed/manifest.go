// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Manifest struct {
	SeededAt      string         `json:"seeded_at"`
	RunSeedID     string         `json:"run_seed_id"`
	APIKey        string         `json:"api_key"`
	Organizations []Organization `json:"organizations"`
}

// Organization carries the *shape* of its seeded users, not the users themselves.
// Index and UserCount are what UserID/ExternalID need to regenerate every id.
type Organization struct {
	ID         string     `json:"id"`
	Index      int        `json:"index"`
	UserCount  int        `json:"user_count"`
	Categories []Category `json:"categories"`
}

// User carries both halves of a seeded user's identity, because the load test needs
// each for a different thing and they are not interchangeable.
//
// ID is the internal id -- what `users.id` holds, what a JWT's `sub` carries, and what
// the Centrifugo channel `user#<id>` is keyed on. ExternalID is the caller-facing id
// that `POST /v1/send` takes as `to.user_id` and that dispatch resolves back to a row
// via EnsureUser(organization, external_id).
//
// The scenarios used to send the internal id as `to.user_id`. Dispatch dutifully treated
// it as an external id it had never seen, created a *second* user row for it, and the
// inbox worker then published to that new row's channel -- which no VU was subscribed to.
// So every send silently grew the user table and ws_push_e2e_latency recorded nothing.
type User struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
}

// UsersOf regenerates an organization's seeded users from the formula.
func (m *Manifest) UsersOf(o Organization) []User {
	users := make([]User, o.UserCount)
	for i := range users {
		users[i] = User{ID: UserID(m.RunSeedID, o.Index, i), ExternalID: ExternalID(o.Index, i)}
	}
	return users
}

type Category struct {
	ID            string         `json:"id"`
	Subscriptions []Subscription `json:"subscriptions"`
}

type Subscription struct {
	ID        string     `json:"id"`
	Templates []Template `json:"templates"`
}

type Template struct {
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Channels []string `json:"channels"`
}

func (m *Manifest) Write(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}
