// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Package apikeybootstrap creates the first API key for a fresh installation.
//
// # Why this exists
//
// Every Hermes admin endpoint sits behind auth.APIKeyMiddleware, and creating an API key is
// itself an admin endpoint requiring apikeys:manage. A cluster with no keys therefore has no
// way to get one: `helm install` completes, every pod reports Ready, and the operator cannot
// make a single authenticated call. Before this package the only escape was hand-computing an
// HMAC and writing a row with psql.
//
// # What it does not do
//
// It creates no organization. Under ADR 0012 an organization is a customer, not a scope on the
// key -- api_keys has no organization_id column and the organization travels as a per-request
// parameter. Seeding a "Default" organization would ship a fake customer, which is exactly the
// conflation that ADR exists to prevent. The first authenticated call is
// `GET /v1/organizations`, which correctly returns an empty list.
//
// # Named apart from internal/bootstrap
//
// internal/bootstrap is service wiring -- lifecycle, serving, metrics. Unrelated.
package apikeybootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/hermesnotifications/hermes/internal/auth"
)

// SecretKey is the key within the Kubernetes Secret that holds the raw API key. It matches the
// environment variable a caller would export, so `kubectl get secret ... -o jsonpath` output can
// be used directly.
const SecretKey = "HERMES_API_KEY"

// KeyStore is the database side: the two operations this needs from the api_keys table.
type KeyStore interface {
	// APIKeyExists reports whether a row with this id is present.
	APIKeyExists(ctx context.Context, keyID string) (bool, error)
	// InsertAPIKey writes the row. It must be a no-op on an id that already exists, so that
	// two Jobs racing produce one key rather than an error.
	InsertAPIKey(ctx context.Context, keyID, keyHash, name string, permissions []string) error
}

// SecretStore is the Kubernetes side. Deliberately two methods and no update: rotation is an
// operator action, not something this Job may do on its own.
type SecretStore interface {
	// GetSecret returns the Secret's data, or found=false if it does not exist.
	GetSecret(ctx context.Context, name string) (data map[string][]byte, found bool, err error)
	// CreateSecret creates it. It must return ErrAlreadyExists rather than overwriting, which
	// is what makes two concurrent Jobs converge instead of clobbering each other.
	CreateSecret(ctx context.Context, name string, data map[string][]byte) error
}

// ErrAlreadyExists is returned by SecretStore.CreateSecret when the Secret was created by
// someone else between the Get and the Create.
var ErrAlreadyExists = errors.New("secret already exists")

// Options configures a run.
type Options struct {
	// SecretName is the Kubernetes Secret holding the key.
	SecretName string
	// KeyName is the human label stored on the api_keys row.
	KeyName string
	// HMACSecret is HERMES_API_KEY_HMAC_SECRET. The stored hash is worthless without it, so a
	// deployment that changes it invalidates every key including this one.
	HMACSecret string
	// ExistingKey, when set, is a raw key the operator supplied themselves. The run then only
	// ensures the database row exists and never touches the Secret -- which is what lets a
	// cluster whose policy forbids workloads writing Secrets still bootstrap, via ESO, Vault
	// or SOPS.
	ExistingKey string
}

// Outcome describes what a run did, for logging. The raw key is deliberately absent: it must
// reach the operator through the Secret, never through logs, because Job pods are retained on
// purpose here and their logs outlive them.
type Outcome struct {
	// KeyID is the api_keys row id. Safe to log -- it is not a credential.
	KeyID string
	// Action is one of "created", "readopted", "present" or "supplied".
	Action string
}

// Run makes a usable first API key exist, and is safe to run repeatedly.
//
// The four paths, in the order they are checked:
//
//	supplied  -- the operator gave us a key; ensure the row, touch no Secret
//	present   -- Secret and row both exist; do nothing
//	readopted -- Secret exists but the row does not, because the database was restored from
//	             before the key was made or was wiped. Re-insert the SAME hash, so the key the
//	             operator already saved keeps working. This is the property that makes the
//	             design worth its RBAC.
//	created   -- neither exists; make both
func Run(ctx context.Context, keys KeyStore, secrets SecretStore, opts Options) (Outcome, error) {
	if opts.HMACSecret == "" {
		return Outcome{}, errors.New("HMAC secret is empty; the stored hash would be unverifiable")
	}
	if opts.SecretName == "" && opts.ExistingKey == "" {
		return Outcome{}, errors.New("no secret name and no supplied key: nowhere to put the result")
	}

	if opts.ExistingKey != "" {
		keyID, err := ensureRow(ctx, keys, opts.ExistingKey, opts)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{KeyID: keyID, Action: "supplied"}, nil
	}

	data, found, err := secrets.GetSecret(ctx, opts.SecretName)
	if err != nil {
		return Outcome{}, fmt.Errorf("read secret %s: %w", opts.SecretName, err)
	}
	if found {
		raw := string(data[SecretKey])
		if raw == "" {
			// A Secret by the right name with the wrong shape. Refuse rather than create a
			// second key beside it: overwriting is not ours to do, and silently minting
			// another leaves two live credentials where the operator expects one.
			return Outcome{}, fmt.Errorf(
				"secret %s exists but has no %s key; delete it or set bootstrap.existingSecret",
				opts.SecretName, SecretKey)
		}
		keyID, _, err := auth.ParseAPIKey(raw)
		if err != nil {
			return Outcome{}, fmt.Errorf("secret %s holds an unparseable key: %w", opts.SecretName, err)
		}
		exists, err := keys.APIKeyExists(ctx, keyID)
		if err != nil {
			return Outcome{}, fmt.Errorf("look up key %s: %w", keyID, err)
		}
		if exists {
			return Outcome{KeyID: keyID, Action: "present"}, nil
		}
		if _, err := ensureRow(ctx, keys, raw, opts); err != nil {
			return Outcome{}, err
		}
		return Outcome{KeyID: keyID, Action: "readopted"}, nil
	}

	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		return Outcome{}, fmt.Errorf("generate api key: %w", err)
	}
	if _, err := ensureRow(ctx, keys, raw, opts); err != nil {
		return Outcome{}, err
	}

	err = secrets.CreateSecret(ctx, opts.SecretName, map[string][]byte{SecretKey: []byte(raw)})
	if errors.Is(err, ErrAlreadyExists) {
		// Another Job won the race between our Get and our Create. Theirs is the key of
		// record; ours is now an orphaned row, harmless but real, and the operator should
		// know a spare credential exists rather than have it left unmentioned.
		return Outcome{KeyID: keyID, Action: "raced"}, nil
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("create secret %s: %w", opts.SecretName, err)
	}
	return Outcome{KeyID: keyID, Action: "created"}, nil
}

// ensureRow inserts the api_keys row for a raw key, tolerating an existing id.
func ensureRow(ctx context.Context, keys KeyStore, raw string, opts Options) (string, error) {
	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		return "", fmt.Errorf("parse api key: %w", err)
	}
	// auth.AllPermissions, not auth.DefaultPermissions: the latter omits apikeys:manage, which
	// would leave the bootstrap key unable to mint the narrower key that replaces it -- the one
	// thing it exists to do.
	if err := keys.InsertAPIKey(ctx, keyID, auth.HMACHashAPIKey(secret, opts.HMACSecret),
		opts.KeyName, auth.AllPermissions); err != nil {
		return "", fmt.Errorf("insert api key %s: %w", keyID, err)
	}
	return keyID, nil
}
